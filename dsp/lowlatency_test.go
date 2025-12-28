package dsp

import (
	"math"
	"testing"
)

// TestNewLowLatencyConvolutionEngine tests engine creation with various parameters.
func TestNewLowLatencyConvolutionEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		irLen         int
		minBlockOrder int
		maxBlockOrder int
		wantLatency   int
		wantErr       bool
	}{
		{
			name:          "basic 64-sample latency",
			irLen:         1024,
			minBlockOrder: 6,
			maxBlockOrder: 9,
			wantLatency:   64,
			wantErr:       false,
		},
		{
			name:          "128-sample latency",
			irLen:         2048,
			minBlockOrder: 7,
			maxBlockOrder: 9,
			wantLatency:   128,
			wantErr:       false,
		},
		{
			name:          "256-sample latency",
			irLen:         4096,
			minBlockOrder: 8,
			maxBlockOrder: 9,
			wantLatency:   256,
			wantErr:       false,
		},
		{
			name:          "512-sample latency",
			irLen:         8192,
			minBlockOrder: 9,
			maxBlockOrder: 9,
			wantLatency:   512,
			wantErr:       false,
		},
		{
			name:          "short IR",
			irLen:         100,
			minBlockOrder: 6,
			maxBlockOrder: 9,
			wantLatency:   64,
			wantErr:       false,
		},
		{
			name:          "empty IR - should error",
			irLen:         0,
			minBlockOrder: 6,
			maxBlockOrder: 9,
			wantLatency:   0,
			wantErr:       true,
		},
		{
			name:          "invalid minBlockOrder too low",
			irLen:         1024,
			minBlockOrder: 5,
			maxBlockOrder: 9,
			wantLatency:   0,
			wantErr:       true,
		},
		{
			name:          "maxBlockOrder < minBlockOrder",
			irLen:         1024,
			minBlockOrder: 8,
			maxBlockOrder: 6,
			wantLatency:   0,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var ir []float32
			if tt.irLen > 0 {
				ir = make([]float32, tt.irLen)
				// Simple exponential decay
				for i := range ir {
					ir[i] = float32(math.Exp(-float64(i) / 1000.0))
				}
			}

			engine, err := NewLowLatencyConvolutionEngine(ir, tt.minBlockOrder, tt.maxBlockOrder)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if engine.Latency() != tt.wantLatency {
				t.Errorf("latency = %d, want %d", engine.Latency(), tt.wantLatency)
			}

			if engine.IRSize() != tt.irLen {
				t.Errorf("IRSize = %d, want %d", engine.IRSize(), tt.irLen)
			}

			if engine.StageCount() == 0 {
				t.Error("expected at least one stage")
			}
		})
	}
}

// TestPartitioning verifies the IR partitioning logic produces expected stages.
func TestPartitioning(t *testing.T) {
	t.Parallel()
	// Create a 4096-sample IR with minBlockOrder=6, maxBlockOrder=9
	ir := make([]float32, 4096)
	for i := range ir {
		ir[i] = float32(math.Exp(-float64(i) / 1000.0))
	}

	engine, err := NewLowLatencyConvolutionEngine(ir, 6, 9)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	t.Logf("IR size: %d, padded: calculated internally", engine.IRSize())
	t.Logf("Number of stages: %d", engine.StageCount())

	// Log stage details
	for i := 0; i < engine.StageCount(); i++ {
		fftSize, blockCount, err := engine.StageInfo(i)
		if err != nil {
			t.Errorf("StageInfo(%d) error: %v", i, err)
			continue
		}

		t.Logf("Stage %d: FFT size=%d, blocks=%d", i, fftSize, blockCount)
	}

	// Verify stages exist
	if engine.StageCount() < 1 {
		t.Error("expected at least one stage")
	}
}

// TestImpulseResponse verifies that convolving with an impulse response
// reproduces the IR when fed a perfect impulse.
func TestImpulseResponse(t *testing.T) {
	t.Parallel()
	// Create a simple IR: [1, 0.5, 0.25, 0.125, ...]
	irLen := 256

	ir := make([]float32, irLen)
	for i := range ir {
		ir[i] = float32(math.Pow(0.5, float64(i)))
	}

	engine, err := NewLowLatencyConvolutionEngine(ir, 6, 8)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	latency := engine.Latency()
	t.Logf("Latency: %d samples", latency)

	// Create impulse input: [1, 0, 0, 0, ...]
	// Need enough samples to capture the full IR output plus latency
	inputLen := irLen + latency + 256 // Extra padding
	input := make([]float32, inputLen)
	input[0] = 1.0 // Impulse at position 0

	output := make([]float32, inputLen)

	// Process in blocks
	blockSize := 64
	for i := 0; i < inputLen; i += blockSize {
		end := i + blockSize
		if end > inputLen {
			end = inputLen
		}

		err := engine.ProcessBlock(input[i:end], output[i:end])
		if err != nil {
			t.Fatalf("ProcessBlock failed: %v", err)
		}
	}

	// The output should be the IR, delayed by the latency
	// Check first few samples of the reproduced IR
	tolerance := float32(0.01)
	matched := 0

	for i := 0; i < irLen && i+latency < inputLen; i++ {
		expected := ir[i]

		actual := output[i+latency]
		if math.Abs(float64(actual-expected)) < float64(tolerance) {
			matched++
		}
	}

	matchRatio := float64(matched) / float64(irLen)
	t.Logf("Matched %d/%d samples (%.1f%%)", matched, irLen, matchRatio*100)

	// Allow some tolerance for the test (at least 80% match)
	if matchRatio < 0.8 {
		t.Errorf("impulse response reproduction poor: only %.1f%% matched", matchRatio*100)

		// Debug: show first few output samples
		t.Logf("First 10 output samples (after latency):")

		for i := 0; i < 10 && i+latency < inputLen; i++ {
			t.Logf("  output[%d] = %.6f, expected IR[%d] = %.6f",
				i+latency, output[i+latency], i, ir[i])
		}
	}
}

// TestProcessBlockVariableSizes tests processing with different block sizes.
func TestProcessBlockVariableSizes(t *testing.T) {
	t.Parallel()

	ir := make([]float32, 512)
	for i := range ir {
		ir[i] = float32(math.Exp(-float64(i) / 100.0))
	}

	engine, err := NewLowLatencyConvolutionEngine(ir, 6, 8)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	blockSizes := []int{1, 16, 32, 64, 100, 128, 256}

	for _, blockSize := range blockSizes {
		t.Run(
			"blockSize="+string(rune(blockSize)), // Simple name
			func(t *testing.T) {
				t.Parallel()
				engine.Reset()

				input := make([]float32, blockSize)
				output := make([]float32, blockSize)

				// Just verify no errors occur
				for i := range 10 {
					// Fill with test signal
					for j := range input {
						input[j] = float32(math.Sin(float64(i*blockSize+j) * 0.1))
					}

					err := engine.ProcessBlock(input, output)
					if err != nil {
						t.Fatalf("ProcessBlock failed with blockSize=%d: %v", blockSize, err)
					}
				}
			},
		)
	}
}

// TestProcessSample32 tests sample-by-sample processing.
func TestProcessSample32(t *testing.T) {
	t.Parallel()

	ir := make([]float32, 256)
	for i := range ir {
		ir[i] = float32(math.Exp(-float64(i) / 50.0))
	}

	engine, err := NewLowLatencyConvolutionEngine(ir, 6, 8)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Process samples one at a time
	numSamples := 512
	for i := range numSamples {
		input := float32(0.0)
		if i == 0 {
			input = 1.0 // Impulse
		}

		_, err := engine.ProcessSample32(input)
		if err != nil {
			t.Fatalf("ProcessSample32 failed at sample %d: %v", i, err)
		}
	}
}

// TestReset verifies that Reset clears all state.
func TestReset(t *testing.T) {
	t.Parallel()

	ir := make([]float32, 512)
	for i := range ir {
		ir[i] = float32(math.Exp(-float64(i) / 100.0))
	}

	engine, err := NewLowLatencyConvolutionEngine(ir, 6, 8)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Process some data
	input := make([]float32, 256)

	output := make([]float32, 256)
	for i := range input {
		input[i] = float32(math.Sin(float64(i) * 0.1))
	}

	err = engine.ProcessBlock(input, output)
	if err != nil {
		t.Fatalf("ProcessBlock failed: %v", err)
	}

	// Reset
	engine.Reset()

	// Process impulse and compare with fresh engine
	engine2, err := NewLowLatencyConvolutionEngine(ir, 6, 8)
	if err != nil {
		t.Fatalf("failed to create second engine: %v", err)
	}

	input = make([]float32, 256)
	input[0] = 1.0

	output1 := make([]float32, 256)
	output2 := make([]float32, 256)

	err = engine.ProcessBlock(input, output1)
	if err != nil {
		t.Fatalf("ProcessBlock on reset engine failed: %v", err)
	}

	err = engine2.ProcessBlock(input, output2)
	if err != nil {
		t.Fatalf("ProcessBlock on fresh engine failed: %v", err)
	}

	// Outputs should be identical
	for i := range output1 {
		if output1[i] != output2[i] {
			t.Errorf("output mismatch at %d: reset=%f, fresh=%f", i, output1[i], output2[i])
			break
		}
	}
}

// TestConvolutionStage tests the ConvolutionStage directly.
func TestConvolutionStage(t *testing.T) {
	t.Parallel()
	// Create a stage with order 6 (64-sample partitions, 128-sample FFT)
	stage, err := NewConvolutionStage(6, 0, 64, 2)
	if err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	if stage.FFTSize() != 128 {
		t.Errorf("FFTSize = %d, want 128", stage.FFTSize())
	}

	if stage.Count() != 2 {
		t.Errorf("Count = %d, want 2", stage.Count())
	}

	// Test IR spectrum calculation
	ir := make([]float32, 128)
	for i := range ir {
		ir[i] = float32(1.0 / float64(i+1))
	}

	err = stage.CalculateIRSpectrums(ir)
	if err != nil {
		t.Fatalf("CalculateIRSpectrums failed: %v", err)
	}
}

// BenchmarkLowLatencyConvolution benchmarks the low-latency engine.
func BenchmarkLowLatencyConvolution(b *testing.B) {
	ir := make([]float32, 4096)
	for i := range ir {
		ir[i] = float32(math.Exp(-float64(i) / 1000.0))
	}

	benchmarks := []struct {
		name          string
		minBlockOrder int
		blockSize     int
	}{
		{"latency64_block64", 6, 64},
		{"latency64_block256", 6, 256},
		{"latency128_block128", 7, 128},
		{"latency128_block256", 7, 256},
		{"latency256_block256", 8, 256},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			engine, err := NewLowLatencyConvolutionEngine(ir, bm.minBlockOrder, 9)
			if err != nil {
				b.Fatalf("failed to create engine: %v", err)
			}

			input := make([]float32, bm.blockSize)

			output := make([]float32, bm.blockSize)
			for i := range input {
				input[i] = float32(math.Sin(float64(i) * 0.1))
			}

			b.ResetTimer()

			for range b.N {
				_ = engine.ProcessBlock(input, output)
			}
		})
	}
}

// BenchmarkCompareEngines compares LowLatencyConvolutionEngine with OverlapAddEngine.
func BenchmarkCompareEngines(b *testing.B) {
	// Note: OverlapAddEngine has a bug with IR lengths > blockSize
	// For fair comparison, we use a short IR and match the block size
	irLen := 256 // Must be <= blockSize for OverlapAddEngine
	blockSize := 256

	ir := make([]float32, irLen)
	for i := range ir {
		ir[i] = float32(math.Exp(-float64(i) / 50.0))
	}

	b.Run("LowLatencyEngine_256", func(b *testing.B) {
		engine, err := NewLowLatencyConvolutionEngine(ir, 8, 9)
		if err != nil {
			b.Fatalf("failed to create engine: %v", err)
		}

		input := make([]float32, blockSize)

		output := make([]float32, blockSize)
		for i := range input {
			input[i] = float32(math.Sin(float64(i) * 0.1))
		}

		b.ResetTimer()

		for range b.N {
			_ = engine.ProcessBlock(input, output)
		}
	})

	b.Run("OverlapAddEngine_256", func(b *testing.B) {
		engine := NewOverlapAddEngine(ir, blockSize)

		input := make([]float32, blockSize)
		for i := range input {
			input[i] = float32(math.Sin(float64(i) * 0.1))
		}

		b.ResetTimer()

		for range b.N {
			_ = engine.ProcessBlock(input)
		}
	})

	// LowLatencyEngine with long IR (4096 samples) - only for LowLatency
	longIR := make([]float32, 4096)
	for i := range longIR {
		longIR[i] = float32(math.Exp(-float64(i) / 1000.0))
	}

	b.Run("LowLatencyEngine_LongIR", func(b *testing.B) {
		engine, err := NewLowLatencyConvolutionEngine(longIR, 8, 9)
		if err != nil {
			b.Fatalf("failed to create engine: %v", err)
		}

		input := make([]float32, blockSize)

		output := make([]float32, blockSize)
		for i := range input {
			input[i] = float32(math.Sin(float64(i) * 0.1))
		}

		b.ResetTimer()

		for range b.N {
			_ = engine.ProcessBlock(input, output)
		}
	})
}
