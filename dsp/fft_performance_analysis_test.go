package dsp

import (
	"fmt"
	"math"
	"testing"

	algofft "github.com/MeKo-Christian/algo-fft"
)

// BenchmarkExtendedFFTSizes tests a wider range of FFT sizes to identify performance cliffs.
// This includes all power-of-2 sizes from 128 to 65536 to find worst-case scenarios.
func BenchmarkExtendedFFTSizes(b *testing.B) {
	// Test all power-of-2 sizes that might be used in practice
	fftSizes := []int{
		128,    // 2^7  - order 6
		256,    // 2^8  - order 7
		512,    // 2^9  - order 8
		1024,   // 2^10 - order 9
		2048,   // 2^11 - order 10
		4096,   // 2^12 - order 11
		8192,   // 2^13 - order 12
		16384,  // 2^14 - order 13 ⚠️ You're seeing this in production
		32768,  // 2^15 - order 14
		65536,  // 2^16 - order 15
		131072, // 2^17 - order 16 (extreme case)
	}

	for _, size := range fftSizes {
		b.Run(fmt.Sprintf("RealFFT_%d", size), func(b *testing.B) {
			benchmarkRealFFTForwardExtended(b, size)
		})
	}
}

// BenchmarkExtendedRealIFFT tests inverse FFT performance across extended range.
func BenchmarkExtendedRealIFFT(b *testing.B) {
	fftSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072}

	for _, size := range fftSizes {
		b.Run(fmt.Sprintf("RealIFFT_%d", size), func(b *testing.B) {
			benchmarkRealFFTInverseExtended(b, size)
		})
	}
}

// BenchmarkExtendedStageOps tests complete stage operations (the real bottleneck).
func BenchmarkExtendedStageOps(b *testing.B) {
	fftSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}

	for _, size := range fftSizes {
		b.Run(fmt.Sprintf("StageOps_%d", size), func(b *testing.B) {
			benchmarkStageFFTOpsExtended(b, size)
		})
	}
}

// BenchmarkWorstCaseScenarios specifically targets sizes that might perform poorly.
// Tests non-power-of-2 sizes near problematic boundaries and specific cache-unfriendly sizes.
func BenchmarkWorstCaseScenarios(b *testing.B) {
	// Test sizes around transitions that might have performance issues
	scenarios := []struct {
		name string
		size int
	}{
		// Large realistic sizes
		{"16K", 16384},
		{"24K", 24576}, // 16K * 1.5 (non-power-of-2 but realistic)
		{"32K", 32768},
		{"48K", 49152}, // 32K * 1.5
		{"64K", 65536},

		// Sizes that might cause cache issues
		{"L1_Overflow_8K", 8192},
		{"L2_Overflow_32K", 32768},
		{"L3_Boundary_128K", 131072},
	}

	for _, scenario := range scenarios {
		b.Run(fmt.Sprintf("Worst_%s", scenario.name), func(b *testing.B) {
			// Only test if it's a valid FFT size (power of 2)
			if isPowerOf2(scenario.size) {
				benchmarkStageFFTOpsExtended(b, scenario.size)
			} else {
				b.Skipf("Size %d is not power of 2, skipping", scenario.size)
			}
		})
	}
}

// BenchmarkPerformancePerSample measures cost per sample to identify inefficiencies.
func BenchmarkPerformancePerSample(b *testing.B) {
	fftSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}

	for _, size := range fftSizes {
		b.Run(fmt.Sprintf("CostPerSample_%d", size), func(b *testing.B) {
			plan, err := algofft.NewPlanReal32(size)
			if err != nil {
				b.Fatalf("failed to create FFT plan: %v", err)
			}

			spectrumLen := size/2 + 1
			inputTime := make([]float32, size)
			signalFreq := make([]complex64, spectrumLen)
			irSpectrum := make([]complex64, spectrumLen)
			outputTime := make([]float32, size)

			for i := range size {
				inputTime[i] = float32(math.Sin(float64(i) * 0.1))
			}

			for i := range spectrumLen {
				irSpectrum[i] = complex(float32(0.5), 0)
			}

			// Report time per sample processed
			b.SetBytes(4) // 4 bytes per float32 sample
			b.ResetTimer()

			for range b.N {
				_ = plan.Forward(signalFreq, inputTime)
				for i := range spectrumLen {
					signalFreq[i] *= irSpectrum[i]
				}
				_ = plan.Inverse(outputTime, signalFreq)
			}

			// Calculate and report ns/sample
			nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			nsPerSample := nsPerOp / float64(size)
			samplesPerSecond := 1e9 / nsPerSample

			b.ReportMetric(nsPerSample, "ns/sample")
			b.ReportMetric(samplesPerSecond/1000000, "MSamples/s")
		})
	}
}

// BenchmarkScalabilityTest shows how performance scales with FFT size.
func BenchmarkScalabilityTest(b *testing.B) {
	// Compare relative performance at key sizes
	baseSizes := []int{
		512,   // Baseline (good performance)
		1024,  // 2x
		2048,  // 4x
		4096,  // 8x
		8192,  // 16x
		16384, // 32x - your problematic size
		32768, // 64x
		65536, // 128x
	}

	for _, size := range baseSizes {
		b.Run(fmt.Sprintf("Scale_%dx", size/512), func(b *testing.B) {
			plan, err := algofft.NewPlanReal32(size)
			if err != nil {
				b.Fatalf("failed to create FFT plan: %v", err)
			}

			spectrumLen := size/2 + 1
			inputTime := make([]float32, size)
			outputFreq := make([]complex64, spectrumLen)

			for i := range size {
				inputTime[i] = float32(math.Sin(float64(i) * 0.1))
			}

			// Report operations per second and time per operation
			b.ResetTimer()

			for range b.N {
				_ = plan.Forward(outputFreq, inputTime)
			}

			nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			opsPerSecond := 1e9 / nsPerOp

			b.ReportMetric(opsPerSecond, "ops/sec")
			b.ReportMetric(nsPerOp/1000, "us/op")
		})
	}
}

// Helper functions

func benchmarkRealFFTForwardExtended(b *testing.B, size int) {
	plan, err := algofft.NewPlanReal32(size)
	if err != nil {
		b.Fatalf("failed to create real FFT plan: %v", err)
	}

	input := make([]float32, size)
	output := make([]complex64, size/2+1)

	for i := range size {
		input[i] = float32(math.Sin(float64(i) * 0.1))
	}

	b.SetBytes(int64(size * 4))
	b.ResetTimer()

	for range b.N {
		err := plan.Forward(output, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRealFFTInverseExtended(b *testing.B, size int) {
	plan, err := algofft.NewPlanReal32(size)
	if err != nil {
		b.Fatalf("failed to create real FFT plan: %v", err)
	}

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

func benchmarkStageFFTOpsExtended(b *testing.B, size int) {
	plan, err := algofft.NewPlanReal32(size)
	if err != nil {
		b.Fatalf("failed to create FFT plan: %v", err)
	}

	spectrumLen := size/2 + 1
	inputTime := make([]float32, size)
	signalFreq := make([]complex64, spectrumLen)
	irSpectrum := make([]complex64, spectrumLen)
	outputTime := make([]float32, size)

	for i := range size {
		inputTime[i] = float32(math.Sin(float64(i) * 0.1))
	}

	for i := range spectrumLen {
		irSpectrum[i] = complex(float32(0.5*math.Exp(-float64(i)/100.0)), 0)
	}

	b.SetBytes(int64(size * 8)) // Forward + inverse
	b.ResetTimer()

	for range b.N {
		err := plan.Forward(signalFreq, inputTime)
		if err != nil {
			b.Fatal(err)
		}

		for i := range spectrumLen {
			signalFreq[i] *= irSpectrum[i]
		}

		err = plan.Inverse(outputTime, signalFreq)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func isPowerOf2(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}
