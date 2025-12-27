package dsp

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	algofft "github.com/MeKo-Christian/algo-fft"
	"pw-convoverb/pkg/irformat"
	"pw-convoverb/pkg/resampler"
)

// IRIndexEntry is an alias for irformat.IndexEntry for external use.
type IRIndexEntry = irformat.IndexEntry

// StateListener is notified when reverb state changes.
// Used by web UI to sync state changes made from TUI.
type StateListener interface {
	OnWetLevelChange(level float64)
	OnDryLevelChange(level float64)
	OnIRChange(index int, name string)
}

// ConvolutionEngine defines the interface for convolution engines.
// Both OverlapAddEngine and LowLatencyConvolutionEngine implement this interface.
type ConvolutionEngine interface {
	// ProcessBlockInplace processes input samples and writes results to output.
	// Input and output must have the same length.
	ProcessBlockInplace(input, output []float32) error

	// Latency returns the processing latency in samples.
	Latency() int

	// Reset clears all internal buffers.
	Reset()
}

// OverlapAddEngine handles FFT-based fast convolution using overlap-add.
type OverlapAddEngine struct {
	// FFT configuration
	fftSize   int // FFT size (should be 2 * blockSize)
	blockSize int // Input block size

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

// EngineType specifies which convolution engine to use.
type EngineType int

const (
	// EngineTypeOverlapAdd uses the simple overlap-add engine.
	// Better for short IRs, higher latency.
	EngineTypeOverlapAdd EngineType = iota

	// EngineTypeLowLatency uses the partitioned low-latency engine.
	// Better for long IRs, configurable latency.
	EngineTypeLowLatency
)

// ConvolutionReverb implements a convolution-based reverb processor.
type ConvolutionReverb struct {
	mu sync.RWMutex

	// Audio configuration
	sampleRate float64
	channels   int

	// Impulse response (resampled to current sample rate)
	ir [][]float32 // IR per channel

	// Original IR (stored at original sample rate for resampling on rate change)
	originalIR         [][]float32
	originalIRRate     float64
	currentIRName      string
	resamplerInstance  *resampler.Resampler
	resamplingInFlight bool // True when async resampling is in progress

	// Mix levels
	wetLevel float64
	dryLevel float64

	// Engine configuration
	engineType    EngineType
	minBlockOrder int // For low-latency engine (6-9)
	maxBlockOrder int // For low-latency engine

	// Convolution engines (per channel)
	engines []ConvolutionEngine

	// Processing state
	enabled bool

	// State listeners (for web UI synchronization)
	listeners []StateListener
}

// NewConvolutionReverb creates a new convolution reverb processor.
// Uses EngineTypeLowLatency by default with 64-sample latency.
func NewConvolutionReverb(sampleRate float64, channels int) *ConvolutionReverb {
	r := &ConvolutionReverb{
		sampleRate:        sampleRate,
		channels:          channels,
		wetLevel:          0.3,
		dryLevel:          0.7,
		engineType:        EngineTypeLowLatency,
		minBlockOrder:     6,     // 64-sample latency
		maxBlockOrder:     9,     // 512-sample max partition
		enabled:           false, // Disabled until IR is loaded
		resamplerInstance: resampler.New(),
	}

	// Initialize per-channel engines slice
	r.engines = make([]ConvolutionEngine, channels)

	return r
}

// NewConvolutionReverbWithEngine creates a new convolution reverb with specified engine type.
func NewConvolutionReverbWithEngine(sampleRate float64, channels int, engineType EngineType) *ConvolutionReverb {
	r := NewConvolutionReverb(sampleRate, channels)
	r.engineType = engineType
	return r
}

// SetEngineType sets the convolution engine type.
// This takes effect on the next LoadImpulseResponse call.
func (r *ConvolutionReverb) SetEngineType(engineType EngineType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.engineType = engineType
}

// SetLatency sets the latency for the low-latency engine.
// Latency is specified as a block order (6=64, 7=128, 8=256, 9=512 samples).
// This takes effect on the next LoadImpulseResponse call.
func (r *ConvolutionReverb) SetLatency(minBlockOrder int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if minBlockOrder < 6 {
		minBlockOrder = 6
	}
	if minBlockOrder > 9 {
		minBlockOrder = 9
	}
	r.minBlockOrder = minBlockOrder
}

// GetLatency returns the current processing latency in samples.
func (r *ConvolutionReverb) GetLatency() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.engines) > 0 && r.engines[0] != nil {
		return r.engines[0].Latency()
	}
	return 1 << r.minBlockOrder
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

// ProcessBlockInplace implements ConvolutionEngine interface.
// It processes input samples and writes results to output.
func (e *OverlapAddEngine) ProcessBlockInplace(input, output []float32) error {
	if len(input) != len(output) {
		return fmt.Errorf("input and output must have same length: %d != %d", len(input), len(output))
	}
	result := e.ProcessBlock(input)
	copy(output, result)
	return nil
}

// Latency implements ConvolutionEngine interface.
// Returns the processing latency in samples (equals block size).
func (e *OverlapAddEngine) Latency() int {
	return e.blockSize
}

// Reset implements ConvolutionEngine interface.
// Clears all internal buffers.
func (e *OverlapAddEngine) Reset() {
	for i := range e.overlapBuffer {
		e.overlapBuffer[i] = 0
	}
	for i := range e.inputBuf {
		e.inputBuf[i] = 0
	}
	for i := range e.outputBuf {
		e.outputBuf[i] = 0
	}
	for i := range e.timeDomainOut {
		e.timeDomainOut[i] = 0
	}
}

// LoadImpulseResponse loads an impulse response from a file.
// Supports .irlib files (IR library format) and falls back to synthetic IR for other files.
// For .irlib files, use LoadImpulseResponseFromLibrary for more control.
func (r *ConvolutionReverb) LoadImpulseResponse(path string) error {
	ext := strings.ToLower(filepath.Ext(path))

	if ext == ".irlib" {
		// Load first IR from library
		return r.LoadImpulseResponseFromLibrary(path, "", 0)
	}

	// Fallback to synthetic IR for backward compatibility
	return r.loadSyntheticIR()
}

// LoadImpulseResponseFromLibrary loads an IR from a library file.
// If irName is non-empty, it loads the IR by name.
// Otherwise, it loads the IR at the given index.
func (r *ConvolutionReverb) LoadImpulseResponseFromLibrary(libraryPath, irName string, irIndex int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Open the library file
	file, err := os.Open(libraryPath)
	if err != nil {
		return fmt.Errorf("failed to open IR library: %w", err)
	}
	defer file.Close()

	// Create reader
	reader, err := irformat.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to read IR library: %w", err)
	}

	// Load the requested IR
	var ir *irformat.ImpulseResponse
	if irName != "" {
		ir, err = reader.LoadIRByName(irName)
		if err != nil {
			return fmt.Errorf("failed to load IR %q: %w", irName, err)
		}
	} else {
		ir, err = reader.LoadIR(irIndex)
		if err != nil {
			return fmt.Errorf("failed to load IR at index %d: %w", irIndex, err)
		}
	}

	// Use the loaded IR data
	return r.applyImpulseResponse(ir.Audio.Data, ir.Metadata.SampleRate)
}

// applyImpulseResponse applies loaded IR data to the reverb engines.
// This method is called with the lock NOT held.
func (r *ConvolutionReverb) applyImpulseResponse(irData [][]float32, irSampleRate float64) error {
	return r.applyImpulseResponseUnlocked(irData, irSampleRate)
}

// applyImpulseResponseUnlocked applies loaded IR data to the reverb engines.
// Caller must hold r.mu lock.
func (r *ConvolutionReverb) applyImpulseResponseUnlocked(irData [][]float32, irSampleRate float64) error {
	if len(irData) == 0 {
		return fmt.Errorf("IR data is empty")
	}

	// Store original IR for future resampling on sample rate changes
	r.originalIR = irData
	r.originalIRRate = irSampleRate

	// Resample IR if sample rates differ
	irToUse := irData
	if irSampleRate != r.sampleRate && r.resamplerInstance != nil {
		log.Printf("Resampling IR from %.0f Hz to %.0f Hz", irSampleRate, r.sampleRate)
		resampled, err := r.resamplerInstance.ResampleMultiChannel(irData, irSampleRate, r.sampleRate)
		if err != nil {
			return fmt.Errorf("failed to resample IR: %w", err)
		}
		irToUse = resampled
	}

	// Handle channel count mismatch
	r.ir = make([][]float32, r.channels)

	for ch := range r.channels {
		if ch < len(irToUse) {
			// Use the corresponding channel from the IR
			r.ir[ch] = irToUse[ch]
		} else {
			// If IR has fewer channels, duplicate the first channel
			r.ir[ch] = irToUse[0]
		}

		// Create engine based on configured type
		var err error
		r.engines[ch], err = r.createEngine(r.ir[ch])
		if err != nil {
			return fmt.Errorf("failed to create engine for channel %d: %w", ch, err)
		}
	}

	r.enabled = true
	return nil
}

// loadSyntheticIR creates a synthetic IR for testing/fallback purposes.
func (r *ConvolutionReverb) loadSyntheticIR() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	irLength := int(r.sampleRate * 2.0) // 2 second IR
	r.ir = make([][]float32, r.channels)

	for ch := range r.channels {
		r.ir[ch] = make([]float32, irLength)
		// Simple exponential decay as placeholder
		for i := range irLength {
			t := float32(i) / float32(r.sampleRate)
			r.ir[ch][i] = float32(0.5 * expApprox(-3.0*t)) // ~1.5s decay time
		}

		// Create engine based on configured type
		var err error
		r.engines[ch], err = r.createEngine(r.ir[ch])
		if err != nil {
			return fmt.Errorf("failed to create engine for channel %d: %w", ch, err)
		}
	}

	r.enabled = true
	return nil
}

// ListLibraryIRs returns the list of IRs available in a library file.
func ListLibraryIRs(libraryPath string) ([]irformat.IndexEntry, error) {
	file, err := os.Open(libraryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open IR library: %w", err)
	}
	defer file.Close()

	reader, err := irformat.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read IR library: %w", err)
	}

	return reader.ListIRs(), nil
}

// ListLibraryIRsFromReader returns the list of IRs available in a library reader.
func ListLibraryIRsFromReader(r io.ReadSeeker) ([]irformat.IndexEntry, error) {
	reader, err := irformat.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read IR library: %w", err)
	}

	return reader.ListIRs(), nil
}

// LoadImpulseResponseFromReader loads an IR from an io.ReadSeeker (e.g., embedded data).
// If irName is non-empty, it loads the IR by name.
// Otherwise, it loads the IR at the given index.
func (r *ConvolutionReverb) LoadImpulseResponseFromReader(reader io.ReadSeeker, irName string, irIndex int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Create irformat reader
	irReader, err := irformat.NewReader(reader)
	if err != nil {
		return fmt.Errorf("failed to read IR library: %w", err)
	}

	// Load the requested IR
	var ir *irformat.ImpulseResponse
	if irName != "" {
		ir, err = irReader.LoadIRByName(irName)
		if err != nil {
			return fmt.Errorf("failed to load IR %q: %w", irName, err)
		}
	} else {
		ir, err = irReader.LoadIR(irIndex)
		if err != nil {
			return fmt.Errorf("failed to load IR at index %d: %w", irIndex, err)
		}
	}

	// Use the loaded IR data
	return r.applyImpulseResponse(ir.Audio.Data, ir.Metadata.SampleRate)
}

// LoadImpulseResponseFromBytes loads an IR from embedded byte data.
// If irName is non-empty, it loads the IR by name.
// Otherwise, it loads the IR at the given index.
func (r *ConvolutionReverb) LoadImpulseResponseFromBytes(data []byte, irName string, irIndex int) error {
	return r.LoadImpulseResponseFromReader(bytes.NewReader(data), irName, irIndex)
}

// SwitchIR switches to a different IR from the embedded library data.
// This is designed for runtime IR switching from the TUI.
// Returns the name of the loaded IR on success.
func (r *ConvolutionReverb) SwitchIR(data []byte, irIndex int) (string, error) {
	reader := bytes.NewReader(data)
	irReader, err := irformat.NewReader(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read IR library: %w", err)
	}

	entries := irReader.ListIRs()
	if irIndex < 0 || irIndex >= len(entries) {
		return "", fmt.Errorf("IR index %d out of range (0-%d)", irIndex, len(entries)-1)
	}

	ir, err := irReader.LoadIR(irIndex)
	if err != nil {
		return "", fmt.Errorf("failed to load IR at index %d: %w", irIndex, err)
	}

	r.mu.Lock()
	if err := r.applyImpulseResponseUnlocked(ir.Audio.Data, ir.Metadata.SampleRate); err != nil {
		r.mu.Unlock()
		return "", err
	}
	listeners := r.listeners
	r.mu.Unlock()

	name := entries[irIndex].Name

	// Notify outside lock
	for _, l := range listeners {
		go l.OnIRChange(irIndex, name)
	}

	return name, nil
}

// createEngine creates a convolution engine based on the configured type.
func (r *ConvolutionReverb) createEngine(ir []float32) (ConvolutionEngine, error) {
	switch r.engineType {
	case EngineTypeLowLatency:
		return NewLowLatencyConvolutionEngine(ir, r.minBlockOrder, r.maxBlockOrder)
	case EngineTypeOverlapAdd:
		// Use block size matching the low-latency engine's latency for fair comparison
		blockSize := 1 << r.minBlockOrder
		return NewOverlapAddEngine(ir, blockSize), nil
	default:
		return NewLowLatencyConvolutionEngine(ir, r.minBlockOrder, r.maxBlockOrder)
	}
}

// SetSampleRate updates the sample rate and triggers async resampling if needed.
func (r *ConvolutionReverb) SetSampleRate(sampleRate float64) {
	r.mu.Lock()

	if sampleRate == r.sampleRate {
		r.mu.Unlock()
		return
	}

	oldRate := r.sampleRate
	r.sampleRate = sampleRate

	// If no original IR is loaded, nothing more to do
	if r.originalIR == nil || r.resamplingInFlight {
		r.mu.Unlock()
		return
	}

	// Mark that resampling is in progress
	r.resamplingInFlight = true

	// Capture what we need for resampling
	originalIR := r.originalIR
	originalIRRate := r.originalIRRate
	resamplerInst := r.resamplerInstance

	r.mu.Unlock()

	// Perform resampling in background goroutine
	go func() {
		log.Printf("Async resampling IR from %.0f Hz to %.0f Hz (rate changed from %.0f Hz)",
			originalIRRate, sampleRate, oldRate)

		resampled, err := resamplerInst.ResampleMultiChannel(originalIR, originalIRRate, sampleRate)
		if err != nil {
			log.Printf("Failed to resample IR: %v", err)
			r.mu.Lock()
			r.resamplingInFlight = false
			r.mu.Unlock()
			return
		}

		r.mu.Lock()
		defer r.mu.Unlock()

		// Check if sample rate changed again while we were resampling
		if r.sampleRate != sampleRate {
			// Rate changed again, don't apply this result
			r.resamplingInFlight = false
			return
		}

		// Apply the resampled IR
		r.ir = make([][]float32, r.channels)
		for ch := range r.channels {
			if ch < len(resampled) {
				r.ir[ch] = resampled[ch]
			} else {
				r.ir[ch] = resampled[0]
			}

			// Recreate engine with resampled IR
			engine, err := r.createEngine(r.ir[ch])
			if err != nil {
				log.Printf("Failed to create engine for channel %d after resampling: %v", ch, err)
				continue
			}
			r.engines[ch] = engine
		}

		r.resamplingInFlight = false
		log.Printf("IR resampling complete, now at %.0f Hz", sampleRate)
	}()
}

// AddStateListener adds a listener for state changes.
func (r *ConvolutionReverb) AddStateListener(l StateListener) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listeners = append(r.listeners, l)
}

// notifyWetLevelChange notifies listeners of a wet level change.
func (r *ConvolutionReverb) notifyWetLevelChange(level float64) {
	for _, l := range r.listeners {
		go l.OnWetLevelChange(level)
	}
}

// notifyDryLevelChange notifies listeners of a dry level change.
func (r *ConvolutionReverb) notifyDryLevelChange(level float64) {
	for _, l := range r.listeners {
		go l.OnDryLevelChange(level)
	}
}

// notifyIRChange notifies listeners of an IR change.
func (r *ConvolutionReverb) notifyIRChange(index int, name string) {
	for _, l := range r.listeners {
		go l.OnIRChange(index, name)
	}
}

// SetWetLevel sets the wet (reverb) mix level (0.0-1.0).
func (r *ConvolutionReverb) SetWetLevel(level float64) {
	r.mu.Lock()
	if level < 0.0 {
		level = 0.0
	}
	if level > 1.0 {
		level = 1.0
	}
	r.wetLevel = level
	listeners := r.listeners
	r.mu.Unlock()

	// Notify outside lock
	for _, l := range listeners {
		go l.OnWetLevelChange(level)
	}
}

// SetDryLevel sets the dry (direct) mix level (0.0-1.0).
func (r *ConvolutionReverb) SetDryLevel(level float64) {
	r.mu.Lock()
	if level < 0.0 {
		level = 0.0
	}
	if level > 1.0 {
		level = 1.0
	}
	r.dryLevel = level
	listeners := r.listeners
	r.mu.Unlock()

	// Notify outside lock
	for _, l := range listeners {
		go l.OnDryLevelChange(level)
	}
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

	// Process block using convolution engine
	// Use a temporary buffer for wet signal
	wet := make([]float32, len(input))
	err := r.engines[channel].ProcessBlockInplace(input, wet)
	if err != nil {
		// On error, just copy input to output
		copy(output, input)
		return
	}

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
