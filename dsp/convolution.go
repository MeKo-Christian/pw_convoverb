package dsp

import (
	"fmt"
	"sync"

	"github.com/MeKo-Christian/algo-fft"
)

// OverlapAddEngine handles FFT-based fast convolution using overlap-add.
type OverlapAddEngine struct {
	// FFT configuration
	fftSize   int         // FFT size (should be 2 * blockSize)
	blockSize int         // Input block size

	// FFT plan for forward and inverse transforms
	plan *algofft.Plan[complex64]

	// Pre-computed IR in frequency domain
	irFFT []complex64

	// Overlap-add buffers
	overlapBuffer []float32 // Stores overlap from previous block
	irLen         int       // Impulse response length

	// Scratch buffers for processing
	inputBuf      []complex64
	outputBuf     []complex64
	timeDomainOut []float32
}

// ConvolutionReverb implements a convolution-based reverb processor.
type ConvolutionReverb struct {
	mu sync.RWMutex

	// Audio configuration
	sampleRate float64
	channels   int

	// Impulse response
	ir [][]float32 // IR per channel

	// Mix levels
	wetLevel float64
	dryLevel float64

	// Overlap-add processing (per channel)
	engines []*OverlapAddEngine

	// Processing state
	enabled bool
}

// NewConvolutionReverb creates a new convolution reverb processor.
func NewConvolutionReverb(sampleRate float64, channels int) *ConvolutionReverb {
	r := &ConvolutionReverb{
		sampleRate: sampleRate,
		channels:   channels,
		wetLevel:   0.3,
		dryLevel:   0.7,
		enabled:    false, // Disabled until IR is loaded
	}

	// Initialize per-channel overlap-add engines
	r.engines = make([]*OverlapAddEngine, channels)

	return r
}

// NewOverlapAddEngine creates a new overlap-add engine for a given impulse response.
func NewOverlapAddEngine(ir []float32, blockSize int) *OverlapAddEngine {
	irLen := len(ir)
	fftSize := nextPowerOf2(2*blockSize - 1)
	if fftSize < irLen {
		fftSize = nextPowerOf2(irLen)
	}

	// Create FFT plan
	plan, err := algofft.NewPlan32(fftSize)
	if err != nil {
		panic(fmt.Sprintf("failed to create FFT plan: %v", err))
	}

	engine := &OverlapAddEngine{
		fftSize:       fftSize,
		blockSize:     blockSize,
		plan:          plan,
		irLen:         irLen,
		irFFT:         make([]complex64, fftSize),
		overlapBuffer: make([]float32, irLen-1),
		inputBuf:      make([]complex64, fftSize),
		outputBuf:     make([]complex64, fftSize),
		timeDomainOut: make([]float32, fftSize),
	}

	// Pre-compute FFT of IR (zero-padded to fftSize)
	irPadded := make([]float32, fftSize)
	copy(irPadded, ir)

	// Forward transform of IR
	irComplex := make([]complex64, fftSize)
	for i, v := range irPadded {
		irComplex[i] = complex(v, 0)
	}

	// Use algo-fft for FFT transform
	err = plan.Forward(engine.irFFT, irComplex)
	if err != nil {
		panic(fmt.Sprintf("failed to compute IR FFT: %v", err))
	}

	return engine
}

// ProcessBlock processes a block of samples using overlap-add.
func (e *OverlapAddEngine) ProcessBlock(input []float32) []float32 {
	if len(input) > e.blockSize {
		panic(fmt.Sprintf("input block size %d exceeds engine block size %d", len(input), e.blockSize))
	}

	// Pad input to FFT size
	for i := 0; i < e.fftSize; i++ {
		if i < len(input) {
			e.inputBuf[i] = complex(input[i], 0)
		} else {
			e.inputBuf[i] = 0
		}
	}

	// Forward FFT of input
	err := e.plan.Forward(e.inputBuf, e.inputBuf)
	if err != nil {
		panic(fmt.Sprintf("forward FFT failed: %v", err))
	}

	// Multiply in frequency domain
	for i := range e.outputBuf {
		e.outputBuf[i] = e.inputBuf[i] * e.irFFT[i]
	}

	// Inverse FFT (algo-fft scales by 1/N automatically)
	err = e.plan.Inverse(e.outputBuf, e.outputBuf)
	if err != nil {
		panic(fmt.Sprintf("inverse FFT failed: %v", err))
	}

	// Convert back to real
	for i := range e.timeDomainOut {
		e.timeDomainOut[i] = real(e.outputBuf[i])
	}

	// Overlap-add: combine with previous overlap
	output := make([]float32, len(input))
	resultLen := len(input) + e.irLen - 1

	// Add overlap from previous block
	for i := 0; i < len(e.overlapBuffer) && i < len(output); i++ {
		output[i] += e.overlapBuffer[i]
	}

	// Add current block's output
	for i := 0; i < len(output); i++ {
		output[i] += e.timeDomainOut[i]
	}

	// Save overlap for next block
	if resultLen > len(input) {
		overlapLen := resultLen - len(input)
		if overlapLen > len(e.overlapBuffer) {
			overlapLen = len(e.overlapBuffer)
		}
		copy(e.overlapBuffer, e.timeDomainOut[len(input):len(input)+overlapLen])
	}

	return output
}

// LoadImpulseResponse loads an impulse response from a WAV file.
func (r *ConvolutionReverb) LoadImpulseResponse(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// TODO: Implement WAV file loading
	// For now, create a simple synthetic IR (e.g., exponential decay)
	irLength := int(r.sampleRate * 2.0) // 2 second IR
	r.ir = make([][]float32, r.channels)

	for ch := range r.channels {
		r.ir[ch] = make([]float32, irLength)
		// Simple exponential decay as placeholder
		for i := range irLength {
			t := float32(i) / float32(r.sampleRate)
			r.ir[ch][i] = float32(0.5 * expApprox(-3.0*t)) // ~1.5s decay time
		}

		// Create overlap-add engine for this channel
		r.engines[ch] = NewOverlapAddEngine(r.ir[ch], 256) // Use 256-sample blocks
	}

	r.enabled = true
	return nil
}

// SetSampleRate updates the sample rate and recalculates coefficients.
func (r *ConvolutionReverb) SetSampleRate(sampleRate float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sampleRate == r.sampleRate {
		return
	}

	r.sampleRate = sampleRate
	// Note: If IR is loaded, it should ideally be resampled
	// For now, we just update the rate
}

// SetWetLevel sets the wet (reverb) mix level (0.0-1.0).
func (r *ConvolutionReverb) SetWetLevel(level float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if level < 0.0 {
		level = 0.0
	}
	if level > 1.0 {
		level = 1.0
	}
	r.wetLevel = level
}

// SetDryLevel sets the dry (direct) mix level (0.0-1.0).
func (r *ConvolutionReverb) SetDryLevel(level float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if level < 0.0 {
		level = 0.0
	}
	if level > 1.0 {
		level = 1.0
	}
	r.dryLevel = level
}

// GetWetLevel returns the current wet level.
func (r *ConvolutionReverb) GetWetLevel() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.wetLevel
}

// GetDryLevel returns the current dry level.
func (r *ConvolutionReverb) GetDryLevel() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dryLevel
}

// ProcessSample processes a single sample through the reverb.
func (r *ConvolutionReverb) ProcessSample(input float32, channel int) float32 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.enabled || channel >= r.channels || len(r.ir[channel]) == 0 {
		return input
	}

	// For sample-by-sample processing, we just pass through
	// Real processing happens in ProcessBlock with overlap-add
	dry := input * float32(r.dryLevel)
	return dry
}

// ProcessBlock processes a block of samples for a specific channel.
func (r *ConvolutionReverb) ProcessBlock(input, output []float32, channel int) {
	if len(input) != len(output) {
		panic(fmt.Sprintf("input and output buffers must have the same length: %d != %d", len(input), len(output)))
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.enabled || channel >= r.channels || r.engines[channel] == nil {
		copy(output, input)
		return
	}

	// Process block using overlap-add with optimized FFT
	wet := r.engines[channel].ProcessBlock(input)

	// Mix dry and wet
	for i := range output {
		dry := input[i] * float32(r.dryLevel)
		wetOut := float32(0)
		if i < len(wet) {
			wetOut = wet[i] * float32(r.wetLevel)
		}
		output[i] = dry + wetOut
	}
}

// GetMetrics returns current processing metrics (for TUI display).
func (r *ConvolutionReverb) GetMetrics(channel int) (inputLevel, outputLevel, reverbLevel float32) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// TODO: Implement proper metering
	// For now, return placeholder values
	return 0.0, 0.0, 0.0
}

// Helper functions

// nextPowerOf2 returns the next power of 2 >= n
func nextPowerOf2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p *= 2
	}
	return p
}


