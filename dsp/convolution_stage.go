package dsp

import (
	"errors"
	"fmt"

	algofft "github.com/MeKo-Christian/algo-fft"
)

// ErrInputBufferTooSmall indicates the input buffer is smaller than required.
var ErrInputBufferTooSmall = errors.New("input buffer too small")

// ConvolutionStage represents a single partition stage in the
// low-latency convolution algorithm. Each stage processes IR blocks
// of the same size at different update rates determined by modulo scheduling.
//
// The partitioned convolution works by:
// 1. Dividing the IR into multiple stages with increasing FFT sizes
// 2. Each stage runs at a different rate (smaller stages run more often)
// 3. This distributes CPU load across multiple blocks while maintaining low latency.
type ConvolutionStage struct {
	// FFT configuration
	fftOrder    int // FFT order (e.g., 7 for 128-sample blocks)
	fftSize     int // FFT size = 2^(fftOrder+1), double the block size
	fftSizeHalf int // Half FFT size = partition size = 2^fftOrder

	// Position tracking
	outputPos int // Starting position in IR array
	latency   int // Minimum latency (from parent engine)

	// Modulo scheduling (determines when this stage executes)
	// Stage only processes when mod == 0
	mod    int // Current modulo counter
	modAnd int // Bitmask for modulo (count-1), e.g., 3 for 4 blocks

	// Pre-computed IR spectrums (frequency domain)
	// Each element represents one FFT-sized partition of the IR
	irSpectrums [][]complex64

	// FFT plan for this stage
	fftPlan *algofft.PlanRealT[float32, complex64]

	// Processing buffers
	signalFreq    []complex64 // Input signal in frequency domain
	convolved     []complex64 // Convolution result (frequency domain)
	convolvedTime []float32   // Convolution result (time domain)
}

// NewConvolutionStage creates a new stage for partitioned convolution.
//
// Parameters:
//   - irOrder: FFT order for this stage (e.g., 6 for 64-sample partitions)
//   - startPos: Starting position in IR array for this stage's data
//   - latency: Minimum latency from parent engine (determines modulo scheduling)
//   - count: Number of IR blocks this stage handles
//
// The FFT size will be 2^(irOrder+1), i.e., double the block size.
func NewConvolutionStage(irOrder int, startPos, latency, count int) (*ConvolutionStage, error) {
	fftSize := 1 << (irOrder + 1)  // 2^(irOrder+1)
	fftSizeHalf := 1 << irOrder    // 2^irOrder
	spectrumLen := fftSizeHalf + 1 // N/2+1 for real FFT

	// Create FFT plan for real-to-complex transforms
	fftPlan, err := algofft.NewPlanReal32(fftSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create FFT plan for size %d: %w", fftSize, err)
	}

	s := &ConvolutionStage{
		fftOrder:    irOrder,
		fftSize:     fftSize,
		fftSizeHalf: fftSizeHalf,
		outputPos:   startPos,
		latency:     latency,
		mod:         0,
		modAnd:      0, // Will be set in CalculateIRSpectrums

		fftPlan: fftPlan,

		// Allocate IR spectrum storage
		irSpectrums: make([][]complex64, count),

		// Allocate processing buffers
		signalFreq:    make([]complex64, spectrumLen),
		convolved:     make([]complex64, spectrumLen),
		convolvedTime: make([]float32, fftSize),
	}

	return s, nil
}

// FFTSize returns the FFT size for this stage.
func (s *ConvolutionStage) FFTSize() int {
	return s.fftSize
}

// Count returns the number of IR blocks in this stage.
func (s *ConvolutionStage) Count() int {
	return len(s.irSpectrums)
}

// CalculateIRSpectrums pre-computes FFT of IR partitions for this stage.
// The IR is partitioned into 'count' blocks, each of size fftSizeHalf.
// Each block is zero-padded to fftSize and transformed to frequency domain.
//
// The IR layout for each block:
//   - First half: zeros (for proper linear convolution via FFT)
//   - Second half: IR data from [outputPos + block*fftSizeHalf]
func (s *ConvolutionStage) CalculateIRSpectrums(impulseResponse []float32) error {
	spectrumLen := s.fftSizeHalf + 1

	// Calculate modulo mask for scheduling
	// If fftSizeHalf is 256 and latency is 64, modAnd = 256/64 - 1 = 3
	// This means the stage runs every 4th latency block
	s.modAnd = (s.fftSizeHalf / s.latency) - 1

	// Temporary buffer for zero-padded IR partition
	tempIR := make([]float32, s.fftSize)

	for blockIdx := range s.irSpectrums {
		// Allocate spectrum storage for this block
		s.irSpectrums[blockIdx] = make([]complex64, spectrumLen)

		// Zero the first half (zero-padding for linear convolution)
		for i := range s.fftSizeHalf {
			tempIR[i] = 0
		}

		// Copy IR data to second half
		srcStart := s.outputPos + blockIdx*s.fftSizeHalf

		srcEnd := srcStart + s.fftSizeHalf
		if srcEnd > len(impulseResponse) {
			srcEnd = len(impulseResponse)
		}

		// Copy available IR data
		copied := 0
		if srcStart < len(impulseResponse) {
			copied = copy(tempIR[s.fftSizeHalf:], impulseResponse[srcStart:srcEnd])
		}

		// Zero-pad remaining if IR is shorter
		for i := s.fftSizeHalf + copied; i < s.fftSize; i++ {
			tempIR[i] = 0
		}

		// Compute FFT of this IR partition
		err := s.fftPlan.Forward(s.irSpectrums[blockIdx], tempIR)
		if err != nil {
			return fmt.Errorf("failed to compute IR spectrum for block %d: %w", blockIdx, err)
		}
	}

	return nil
}

// PerformConvolution executes convolution for this stage.
// Only executes when the modulo counter reaches 0 (modulo scheduling).
//
// Parameters:
//   - signalIn: Input buffer - the stage reads the last fftSize samples
//     (accessed as signalIn[len(signalIn)-fftSize:])
//   - signalOut: Output accumulation buffer where results are overlap-added
//
// The modulo scheduling spreads CPU load across blocks:
//   - Smallest stages (64 samples) run every block
//   - Larger stages run less frequently (every 2nd, 4th, 8th block, etc.)
func (s *ConvolutionStage) PerformConvolution(signalIn, signalOut []float32) error {
	if s.mod == 0 {
		// Extract the last fftSize samples from input buffer
		inputStart := len(signalIn) - s.fftSize
		if inputStart < 0 {
			return fmt.Errorf("%w: need=%d got=%d", ErrInputBufferTooSmall, s.fftSize, len(signalIn))
		}

		// Forward FFT of input signal
		err := s.fftPlan.Forward(s.signalFreq, signalIn[inputStart:inputStart+s.fftSize])
		if err != nil {
			return fmt.Errorf("forward FFT failed: %w", err)
		}

		half := s.fftSizeHalf
		spectrumLen := half + 1

		// Process each IR block at this stage
		for blockIdx, irSpectrum := range s.irSpectrums {
			// Determine destination buffer for complex multiplication
			// If single block, multiply directly into signalFreq
			// Otherwise use convolved buffer to preserve signalFreq for next iteration
			var dest []complex64
			if len(s.irSpectrums) == 1 {
				dest = s.signalFreq
			} else {
				// Copy signalFreq to convolved for multiplication
				copy(s.convolved, s.signalFreq[:spectrumLen])
				dest = s.convolved
			}

			// Complex multiply: signal * IR spectrum
			complexMultiplyInplace(dest, irSpectrum, spectrumLen)

			// Inverse FFT to get time-domain result
			err := s.fftPlan.Inverse(s.convolvedTime, dest)
			if err != nil {
				return fmt.Errorf("inverse FFT failed: %w", err)
			}

			// Overlap-add into output buffer at appropriate position
			// Output position: outputPos + latency - fftSizeHalf + blockIdx * half
			outPos := s.outputPos + s.latency - s.fftSizeHalf + blockIdx*half
			if outPos >= 0 && outPos+half <= len(signalOut) {
				for i := range half {
					signalOut[outPos+i] += s.convolvedTime[i]
				}
			}
		}
	}

	// Update modulo counter
	s.mod = (s.mod + 1) & s.modAnd

	return nil
}

// Reset resets the stage's modulo counter and clears processing buffers.
func (s *ConvolutionStage) Reset() {
	s.mod = 0

	// Clear processing buffers
	for i := range s.signalFreq {
		s.signalFreq[i] = 0
	}

	for i := range s.convolved {
		s.convolved[i] = 0
	}

	for i := range s.convolvedTime {
		s.convolvedTime[i] = 0
	}
}

// complexMultiplyInplace performs element-wise complex multiplication: dest *= src.
func complexMultiplyInplace(dest, src []complex64, n int) {
	for i := range n {
		dest[i] *= src[i]
	}
}
