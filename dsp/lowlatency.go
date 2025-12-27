package dsp

import (
	"fmt"
)

// LowLatencyConvolutionEngine implements partitioned convolution with
// configurable latency. The IR is split into stages with exponentially
// increasing partition sizes for efficient processing of long impulse responses.
//
// Key features:
//   - Latency = 2^minBlockOrder samples (e.g., 64, 128, 256, 512)
//   - IR partitioned into multiple stages with increasing FFT sizes
//   - Modulo scheduling distributes CPU load across blocks
//   - Suitable for real-time audio processing
//
// Based on the algorithm from DAV_DspConvolution.pas (TLowLatencyConvolution32).
type LowLatencyConvolutionEngine struct {
	// IR configuration
	impulseResponse []float32 // Original IR (stored for rebuilding)
	irSize          int       // Actual IR size
	irSizePadded    int       // Padded to align with partition boundaries

	// Latency configuration
	minBlockOrder int // Minimum block order (6-9, determines latency)
	maxBlockOrder int // Maximum block order (must be >= minBlockOrder)
	latency       int // Actual latency = 2^minBlockOrder

	// Ring buffers
	inputBuffer       []float32 // Ring buffer for input history
	outputBuffer      []float32 // Ring buffer for output accumulation
	inputBufferSize   int       // Size = 2 * 2^maxIROrder (depends on IR size)
	inputHistorySize  int       // = inputBufferSize - latency
	outputHistorySize int       // = irSizePadded - latency
	blockPosition     int       // Current position within latency block

	// Convolution stages (partitioned processing)
	stages []*ConvolutionStage
}

// NewLowLatencyConvolutionEngine creates a low-latency convolution engine.
//
// Parameters:
//   - ir: Impulse response (will be copied)
//   - minBlockOrder: Minimum FFT order (6-9), determines latency as 2^minBlockOrder samples
//   - maxBlockOrder: Maximum FFT order (must be >= minBlockOrder)
//
// The latency will be 2^minBlockOrder samples:
//   - minBlockOrder=6 → 64 samples latency
//   - minBlockOrder=7 → 128 samples latency
//   - minBlockOrder=8 → 256 samples latency
//   - minBlockOrder=9 → 512 samples latency
func NewLowLatencyConvolutionEngine(ir []float32, minBlockOrder, maxBlockOrder int) (*LowLatencyConvolutionEngine, error) {
	if minBlockOrder < 6 || minBlockOrder > 12 {
		return nil, fmt.Errorf("minBlockOrder must be between 6 and 12, got %d", minBlockOrder)
	}
	if maxBlockOrder < minBlockOrder {
		return nil, fmt.Errorf("maxBlockOrder (%d) must be >= minBlockOrder (%d)", maxBlockOrder, minBlockOrder)
	}
	if len(ir) == 0 {
		return nil, fmt.Errorf("impulse response cannot be empty")
	}

	e := &LowLatencyConvolutionEngine{
		irSize:        len(ir),
		minBlockOrder: minBlockOrder,
		maxBlockOrder: maxBlockOrder,
		latency:       1 << minBlockOrder, // 2^minBlockOrder
		blockPosition: 0,
	}

	// Copy IR
	e.impulseResponse = make([]float32, len(ir))
	copy(e.impulseResponse, ir)

	// Calculate padded IR size and partition
	e.irSizePadded = e.calculatePaddedIRSize()

	// Partition the IR into stages
	err := e.partitionIR()
	if err != nil {
		return nil, fmt.Errorf("failed to partition IR: %w", err)
	}

	// Build IR spectrums for all stages
	err = e.buildIRSpectrums()
	if err != nil {
		return nil, fmt.Errorf("failed to build IR spectrums: %w", err)
	}

	return e, nil
}

// Latency returns the current latency in samples.
func (e *LowLatencyConvolutionEngine) Latency() int {
	return e.latency
}

// IRSize returns the original IR size.
func (e *LowLatencyConvolutionEngine) IRSize() int {
	return e.irSize
}

// calculatePaddedIRSize computes the padded IR size to align with partition boundaries.
func (e *LowLatencyConvolutionEngine) calculatePaddedIRSize() int {
	if e.irSize == 0 {
		return 0
	}

	minBlockSize := 1 << e.minBlockOrder
	// Round up to multiple of minimum block size
	padded := ((e.irSize + minBlockSize - 1) / minBlockSize) * minBlockSize
	return padded
}

// bitCountToBits returns (2^(bitCount+1)) - 1
// For bitCount=6: returns 127 (2^7 - 1)
func bitCountToBits(bitCount int) int {
	return (2 << bitCount) - 1
}

// truncLog2 returns floor(log2(n))
func truncLog2(n int) int {
	if n <= 0 {
		return 0
	}
	result := 0
	for n > 1 {
		n >>= 1
		result++
	}
	return result
}

// partitionIR divides the IR into stages with increasing FFT sizes.
// This is the core algorithm from the Pascal reference (lines 1397-1445).
//
// The algorithm:
// 1. Calculate the maximum FFT order needed based on IR size
// 2. Allocate at least one block per FFT size from minBlockOrder to maxIROrder
// 3. Distribute remaining IR across stages using bit manipulation
// 4. Create stages with logarithmically increasing sizes
func (e *LowLatencyConvolutionEngine) partitionIR() error {
	// Clear existing stages
	e.stages = nil

	if e.irSizePadded == 0 {
		return nil
	}

	minBlockSize := 1 << e.minBlockOrder

	// Calculate maximum FFT order needed for this IR
	maxIROrd := truncLog2(e.irSizePadded+minBlockSize) - 1

	// At least one block of each FFT size is necessary
	// ResIRSize = irSizePadded - sum of one block per size
	resIRSize := e.irSizePadded - (bitCountToBits(maxIROrd) - bitCountToBits(e.minBlockOrder-1))

	// Check if highest order block is only used once; if not, decrease
	if ((resIRSize&(1<<maxIROrd))>>maxIROrd) == 0 && maxIROrd > e.minBlockOrder {
		maxIROrd--
	}

	// Clip to maximum allowed order
	if maxIROrd > e.maxBlockOrder {
		maxIROrd = e.maxBlockOrder
	}

	// Recalculate residual since maxIROrd could have changed
	resIRSize = e.irSizePadded - (bitCountToBits(maxIROrd) - bitCountToBits(e.minBlockOrder-1))

	// Initialize stage array
	numStages := maxIROrd - e.minBlockOrder + 1
	e.stages = make([]*ConvolutionStage, numStages)

	// Create stages from minBlockOrder to maxIROrd-1
	startPos := 0
	for order := e.minBlockOrder; order < maxIROrd; order++ {
		// Count blocks at this order: 1 mandatory + any from residual
		count := 1 + ((resIRSize & (1 << order)) >> order)

		stage, err := NewConvolutionStage(order, startPos, e.latency, count)
		if err != nil {
			return fmt.Errorf("failed to create stage for order %d: %w", order, err)
		}
		e.stages[order-e.minBlockOrder] = stage

		startPos += count * (1 << order)
		resIRSize -= (count - 1) * (1 << order)
	}

	// Last stage (highest order)
	count := 1 + (resIRSize / (1 << maxIROrd))
	stage, err := NewConvolutionStage(maxIROrd, startPos, e.latency, count)
	if err != nil {
		return fmt.Errorf("failed to create final stage for order %d: %w", maxIROrd, err)
	}
	e.stages[len(e.stages)-1] = stage

	// Update input buffer size to accommodate largest FFT
	e.inputBufferSize = 2 << maxIROrd
	e.inputHistorySize = e.inputBufferSize - e.latency

	// Allocate input buffer
	e.inputBuffer = make([]float32, e.inputBufferSize)

	// Allocate output buffer
	e.outputHistorySize = e.irSizePadded - e.latency
	e.outputBuffer = make([]float32, e.irSizePadded)

	return nil
}

// buildIRSpectrums triggers FFT computation for all stages.
func (e *LowLatencyConvolutionEngine) buildIRSpectrums() error {
	// Pad IR to irSizePadded if needed
	paddedIR := make([]float32, e.irSizePadded)
	copy(paddedIR, e.impulseResponse)

	for i, stage := range e.stages {
		err := stage.CalculateIRSpectrums(paddedIR)
		if err != nil {
			return fmt.Errorf("failed to calculate IR spectrums for stage %d: %w", i, err)
		}
	}

	return nil
}

// ProcessBlockInplace implements ConvolutionEngine interface.
// It processes input samples and writes results to output.
func (e *LowLatencyConvolutionEngine) ProcessBlockInplace(input, output []float32) error {
	return e.ProcessBlock(input, output)
}

// ProcessBlock processes a block of input samples.
// The block can be any size and will be processed in chunks
// that match the engine's latency setting.
//
// This implements the overlap-add convolution with partitioned stages.
func (e *LowLatencyConvolutionEngine) ProcessBlock(input, output []float32) error {
	if len(input) != len(output) {
		return fmt.Errorf("input and output buffers must have same length: %d != %d", len(input), len(output))
	}

	currentPos := 0
	sampleFrames := len(input)

	for currentPos < sampleFrames {
		remaining := sampleFrames - currentPos

		if e.blockPosition+remaining < e.latency {
			// Not enough samples to complete a latency block - just buffer

			// Copy input to ring buffer
			copy(e.inputBuffer[e.inputHistorySize+e.blockPosition:], input[currentPos:currentPos+remaining])

			// Copy output from ring buffer
			copy(output[currentPos:currentPos+remaining], e.outputBuffer[e.blockPosition:e.blockPosition+remaining])

			// Increase block position
			e.blockPosition += remaining
			break
		} else {
			// Have enough samples to complete a latency block
			samplesToProcess := e.latency - e.blockPosition

			// Copy remaining part of latency block to input buffer
			copy(e.inputBuffer[e.inputHistorySize+e.blockPosition:], input[currentPos:currentPos+samplesToProcess])

			// Copy output from output buffer
			copy(output[currentPos:currentPos+samplesToProcess], e.outputBuffer[e.blockPosition:e.blockPosition+samplesToProcess])

			// Shift output buffer: discard used samples, make room for new
			copy(e.outputBuffer, e.outputBuffer[e.latency:e.latency+e.outputHistorySize])

			// Zero out space for new convolution output
			for i := e.outputHistorySize; i < len(e.outputBuffer); i++ {
				e.outputBuffer[i] = 0
			}

			// CORE: Perform partitioned convolution for all stages
			for _, stage := range e.stages {
				// Each stage reads from appropriate position in inputBuffer
				// The stage's PerformConvolution reads the last fftSize samples
				err := stage.PerformConvolution(e.inputBuffer[:e.inputBufferSize], e.outputBuffer)
				if err != nil {
					return fmt.Errorf("stage convolution failed: %w", err)
				}
			}

			// Shift input buffer: discard used samples
			copy(e.inputBuffer, e.inputBuffer[e.latency:e.latency+e.inputHistorySize])

			// Advance position and reset block position
			currentPos += samplesToProcess
			e.blockPosition = 0
		}
	}

	return nil
}

// ProcessSample32 processes a single sample through the engine.
// This is less efficient than ProcessBlock but useful for sample-by-sample processing.
func (e *LowLatencyConvolutionEngine) ProcessSample32(input float32) (float32, error) {
	// Copy input to ring buffer
	e.inputBuffer[e.inputHistorySize+e.blockPosition] = input

	// Get output from output buffer
	output := e.outputBuffer[e.blockPosition]

	// Increase block position
	e.blockPosition++

	if e.blockPosition >= e.latency {
		// Shift output buffer: discard used samples, make room for new
		copy(e.outputBuffer, e.outputBuffer[e.latency:e.latency+e.outputHistorySize])

		// Zero out space for new convolution output
		for i := e.outputHistorySize; i < len(e.outputBuffer); i++ {
			e.outputBuffer[i] = 0
		}

		// Perform partitioned convolution for all stages
		for _, stage := range e.stages {
			err := stage.PerformConvolution(e.inputBuffer[:e.inputBufferSize], e.outputBuffer)
			if err != nil {
				return 0, fmt.Errorf("stage convolution failed: %w", err)
			}
		}

		// Shift input buffer: discard used samples
		copy(e.inputBuffer, e.inputBuffer[e.latency:e.latency+e.inputHistorySize])

		// Reset block position
		e.blockPosition = 0
	}

	return output, nil
}

// Reset clears all buffers and resets the engine state.
func (e *LowLatencyConvolutionEngine) Reset() {
	// Clear input buffer
	for i := range e.inputBuffer {
		e.inputBuffer[i] = 0
	}

	// Clear output buffer
	for i := range e.outputBuffer {
		e.outputBuffer[i] = 0
	}

	// Reset block position
	e.blockPosition = 0

	// Reset all stages
	for _, stage := range e.stages {
		stage.Reset()
	}
}

// StageCount returns the number of convolution stages.
func (e *LowLatencyConvolutionEngine) StageCount() int {
	return len(e.stages)
}

// StageInfo returns information about a specific stage.
func (e *LowLatencyConvolutionEngine) StageInfo(index int) (fftSize, blockCount int, err error) {
	if index < 0 || index >= len(e.stages) {
		return 0, 0, fmt.Errorf("stage index %d out of range [0, %d)", index, len(e.stages))
	}
	stage := e.stages[index]
	return stage.FFTSize(), stage.Count(), nil
}
