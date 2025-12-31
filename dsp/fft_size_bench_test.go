package dsp

import (
	"fmt"
	"math"
	"testing"

	algofft "github.com/MeKo-Christian/algo-fft"
)

// BenchmarkFFTSizes benchmarks FFT operations at different sizes used by the convolution engine.
// The LowLatencyConvolutionEngine uses FFT sizes from 128 (2^7) up to 8192 (2^13) or more,
// depending on the IR length and maxBlockOrder configuration.
func BenchmarkFFTSizes(b *testing.B) {
	// Test FFT sizes that correspond to the stages used in the engine
	// FFT size = 2^(order+1), where order is the partition order
	// Common orders: 6 (64-sample partitions, 128 FFT), 7 (128/256), 8 (256/512), 9 (512/1024), etc.
	fftSizes := []int{
		128,   // order 6: smallest stage (64-sample partitions)
		256,   // order 7: 128-sample partitions
		512,   // order 8: 256-sample partitions (common latency setting)
		1024,  // order 9: 512-sample partitions
		2048,  // order 10: 1024-sample partitions
		4096,  // order 11: 2048-sample partitions
		8192,  // order 12: 4096-sample partitions (large IR)
		16384, // order 13: 8192-sample partitions (very large IR)
	}

	for _, fftSize := range fftSizes {
		b.Run(fmt.Sprintf("FFT_%d", fftSize), func(b *testing.B) {
			benchmarkFFTForward(b, fftSize)
		})
	}
}

// BenchmarkFFTSizesInverse benchmarks inverse FFT operations.
func BenchmarkFFTSizesInverse(b *testing.B) {
	fftSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192, 16384}

	for _, fftSize := range fftSizes {
		b.Run(fmt.Sprintf("IFFT_%d", fftSize), func(b *testing.B) {
			benchmarkFFTInverse(b, fftSize)
		})
	}
}

// BenchmarkFFTSizesRoundtrip benchmarks full forward + inverse FFT operations.
func BenchmarkFFTSizesRoundtrip(b *testing.B) {
	fftSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192, 16384}

	for _, fftSize := range fftSizes {
		b.Run(fmt.Sprintf("Roundtrip_%d", fftSize), func(b *testing.B) {
			benchmarkFFTRoundtrip(b, fftSize)
		})
	}
}

// BenchmarkRealFFTSizes benchmarks real-to-complex FFT operations (used by ConvolutionStage).
func BenchmarkRealFFTSizes(b *testing.B) {
	fftSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192, 16384}

	for _, fftSize := range fftSizes {
		b.Run(fmt.Sprintf("RealFFT_%d", fftSize), func(b *testing.B) {
			benchmarkRealFFTForward(b, fftSize)
		})
	}
}

// BenchmarkRealFFTSizesInverse benchmarks complex-to-real inverse FFT operations.
func BenchmarkRealFFTSizesInverse(b *testing.B) {
	fftSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192, 16384}

	for _, fftSize := range fftSizes {
		b.Run(fmt.Sprintf("RealIFFT_%d", fftSize), func(b *testing.B) {
			benchmarkRealFFTInverse(b, fftSize)
		})
	}
}

// BenchmarkStageFFTOperations benchmarks the complete FFT workflow used in ConvolutionStage.
// This includes forward FFT of input, complex multiplication, and inverse FFT.
func BenchmarkStageFFTOperations(b *testing.B) {
	fftSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192}

	for _, fftSize := range fftSizes {
		b.Run(fmt.Sprintf("StageOps_%d", fftSize), func(b *testing.B) {
			benchmarkStageFFTOps(b, fftSize)
		})
	}
}

// Helper functions for benchmarks

func benchmarkFFTForward(b *testing.B, size int) {
	// Create FFT plan
	plan, err := algofft.NewPlan32(size)
	if err != nil {
		b.Fatalf("failed to create FFT plan: %v", err)
	}

	// Prepare input/output buffers
	input := make([]complex64, size)
	output := make([]complex64, size)

	// Fill with test data
	for i := range size {
		input[i] = complex(float32(math.Sin(float64(i)*0.1)), 0)
	}

	b.SetBytes(int64(size * 8)) // 8 bytes per complex64
	b.ResetTimer()

	for range b.N {
		err := plan.Forward(output, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkFFTInverse(b *testing.B, size int) {
	plan, err := algofft.NewPlan32(size)
	if err != nil {
		b.Fatalf("failed to create FFT plan: %v", err)
	}

	input := make([]complex64, size)
	output := make([]complex64, size)

	for i := range size {
		input[i] = complex(float32(i), 0)
	}

	b.SetBytes(int64(size * 8))
	b.ResetTimer()

	for range b.N {
		err := plan.Inverse(output, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkFFTRoundtrip(b *testing.B, size int) {
	plan, err := algofft.NewPlan32(size)
	if err != nil {
		b.Fatalf("failed to create FFT plan: %v", err)
	}

	input := make([]complex64, size)
	temp := make([]complex64, size)
	output := make([]complex64, size)

	for i := range size {
		input[i] = complex(float32(math.Sin(float64(i)*0.1)), 0)
	}

	b.SetBytes(int64(size * 16)) // Two FFTs: 16 bytes per sample
	b.ResetTimer()

	for range b.N {
		err := plan.Forward(temp, input)
		if err != nil {
			b.Fatal(err)
		}

		err = plan.Inverse(output, temp)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRealFFTForward(b *testing.B, size int) {
	// Real FFT plan
	plan, err := algofft.NewPlanReal32(size)
	if err != nil {
		b.Fatalf("failed to create real FFT plan: %v", err)
	}

	// Real input, complex output (N/2+1 elements)
	input := make([]float32, size)
	output := make([]complex64, size/2+1)

	for i := range size {
		input[i] = float32(math.Sin(float64(i) * 0.1))
	}

	b.SetBytes(int64(size * 4)) // 4 bytes per float32
	b.ResetTimer()

	for range b.N {
		err := plan.Forward(output, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRealFFTInverse(b *testing.B, size int) {
	plan, err := algofft.NewPlanReal32(size)
	if err != nil {
		b.Fatalf("failed to create real FFT plan: %v", err)
	}

	// Complex input (N/2+1), real output
	input := make([]complex64, size/2+1)
	output := make([]float32, size)

	for i := range len(input) {
		input[i] = complex(float32(i), 0)
	}

	b.SetBytes(int64(size * 4))
	b.ResetTimer()

	for range b.N {
		err := plan.Inverse(output, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkStageFFTOps(b *testing.B, size int) {
	// This simulates the complete operation in ConvolutionStage.PerformConvolution:
	// 1. Forward FFT of input signal
	// 2. Complex multiplication with IR spectrum
	// 3. Inverse FFT to get time-domain result

	plan, err := algofft.NewPlanReal32(size)
	if err != nil {
		b.Fatalf("failed to create FFT plan: %v", err)
	}

	spectrumLen := size/2 + 1

	// Buffers (as in ConvolutionStage)
	inputTime := make([]float32, size)
	signalFreq := make([]complex64, spectrumLen)
	irSpectrum := make([]complex64, spectrumLen)
	outputTime := make([]float32, size)

	// Prepare input signal
	for i := range size {
		inputTime[i] = float32(math.Sin(float64(i) * 0.1))
	}

	// Prepare IR spectrum (simulate pre-computed IR FFT)
	for i := range spectrumLen {
		irSpectrum[i] = complex(float32(0.5*math.Exp(-float64(i)/100.0)), 0)
	}

	b.SetBytes(int64(size * 8)) // Forward + inverse FFT
	b.ResetTimer()

	for range b.N {
		// Forward FFT of input
		err := plan.Forward(signalFreq, inputTime)
		if err != nil {
			b.Fatal(err)
		}

		// Complex multiplication (element-wise)
		for i := range spectrumLen {
			signalFreq[i] *= irSpectrum[i]
		}

		// Inverse FFT
		err = plan.Inverse(outputTime, signalFreq)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkActualStageUsage benchmarks realistic stage configurations.
func BenchmarkActualStageUsage(b *testing.B) {
	// These configurations mirror actual usage in LowLatencyConvolutionEngine
	configs := []struct {
		name      string
		irOrder   int  // Stage order
		count     int  // Number of IR blocks in this stage
		latency   int  // Engine latency
		startPos  int  // Starting position in IR
	}{
		{"Stage_Order6_1Block", 6, 1, 64, 0},      // Smallest stage: 64-sample partitions, 128 FFT
		{"Stage_Order7_2Blocks", 7, 2, 64, 64},    // 128-sample partitions, 256 FFT
		{"Stage_Order8_2Blocks", 8, 2, 256, 256},  // 256-sample partitions, 512 FFT (common)
		{"Stage_Order9_4Blocks", 9, 4, 256, 512},  // 512-sample partitions, 1024 FFT
		{"Stage_Order10_4Blocks", 10, 4, 256, 2048}, // 1024-sample partitions, 2048 FFT
		{"Stage_Order11_8Blocks", 11, 8, 256, 4096}, // 2048-sample partitions, 4096 FFT (large IR)
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			// Create IR long enough for this stage
			irLen := cfg.startPos + (cfg.count * (1 << cfg.irOrder))
			ir := make([]float32, irLen)
			for i := range ir {
				ir[i] = float32(math.Exp(-float64(i) / 1000.0))
			}

			// Create the actual stage
			stage, err := NewConvolutionStage(cfg.irOrder, cfg.startPos, cfg.latency, cfg.count)
			if err != nil {
				b.Fatalf("failed to create stage: %v", err)
			}

			// Calculate IR spectrums (pre-computation)
			err = stage.CalculateIRSpectrums(ir)
			if err != nil {
				b.Fatalf("failed to calculate IR spectrums: %v", err)
			}

			// Prepare input buffer (simulates ring buffer in engine)
			fftSize := stage.FFTSize()
			inputBuffer := make([]float32, fftSize*2)
			for i := range inputBuffer {
				inputBuffer[i] = float32(math.Sin(float64(i) * 0.1))
			}

			// Prepare output buffer
			outputBuffer := make([]float32, irLen)

			b.SetBytes(int64(fftSize * 4 * cfg.count)) // Approximate work done
			b.ResetTimer()

			for range b.N {
				err := stage.PerformConvolution(inputBuffer, outputBuffer)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
