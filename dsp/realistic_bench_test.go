package dsp

import (
	"math"
	"math/rand"
	"testing"
)

// Run these benchmarks with:
//   go test ./dsp -run ^$ -bench Realistic -benchmem
//
// Notes:
// - The LowLatency engine work scales strongly with IR length and chosen latency.
// - ConvolutionReverb.ProcessBlock currently allocates a per-call wet buffer; the
//   benchmark includes a variant to make that visible via -benchmem.

func generateRealisticIR(sampleRate int, seconds float64, channels int) [][]float32 {
	if channels <= 0 {
		channels = 1
	}

	n := int(seconds * float64(sampleRate))
	if n < 1 {
		n = 1
	}

	// A simple “room-ish” IR model:
	// - a few early reflections (short taps)
	// - an exponential noisy tail (RT60-ish)
	rng := rand.New(rand.NewSource(1))

	// Rough RT60: scale so the tail is mostly gone by `seconds`.
	// This is not physically exact; it’s just a stable, realistic workload.
	rt60 := math.Max(0.15, seconds*0.75)
	decayK := 6.907755278982137 / rt60 // ln(1000) / RT60

	earlyMs := []float64{0, 2.3, 4.7, 7.1, 11.3, 17.9, 29.7}
	earlyGains := []float64{1.0, 0.55, 0.42, 0.32, 0.22, 0.14, 0.08}

	ir := make([][]float32, channels)
	for ch := range channels {
		buf := make([]float32, n)

		// Early reflections.
		for i := range earlyMs {
			idx := int((earlyMs[i] / 1000.0) * float64(sampleRate))
			if idx >= 0 && idx < n {
				stereoSkew := 1.0

				if channels > 1 {
					// Make L/R slightly different to avoid perfect correlation.
					if ch%2 == 0 {
						stereoSkew = 0.97
					} else {
						stereoSkew = 1.03
					}
				}

				sign := float32(1)
				if (i+ch)%2 == 1 {
					sign = -1
				}

				buf[idx] += sign * float32(earlyGains[i]*stereoSkew)
			}
		}

		// Noisy tail.
		for i := range n {
			t := float64(i) / float64(sampleRate)
			env := math.Exp(-decayK * t)
			noise := (rng.Float64()*2 - 1) * 0.02
			buf[i] += float32(env * noise)
		}

		ir[ch] = buf
	}

	return ir
}

func generateTestInput(blockSize int) []float32 {
	// A stable, deterministic “music-ish” signal (2 sines + tiny noise).
	rng := rand.New(rand.NewSource(2))

	in := make([]float32, blockSize)
	for i := range blockSize {
		s := 0.6*math.Sin(float64(i)*2*math.Pi*440/48000.0) + 0.3*math.Sin(float64(i)*2*math.Pi*1100/48000.0)
		s += (rng.Float64()*2 - 1) * 0.001
		in[i] = float32(s)
	}

	return in
}

func BenchmarkRealisticLowLatencyEngine_Stereo(b *testing.B) {
	const sampleRate = 48000
	const channels = 2

	testCases := []struct {
		name          string
		seconds       float64
		blockSize     int
		minBlockOrder int // latency = 2^minBlockOrder
		maxBlockOrder int
	}{
		{"ir1.0s_block256_lat256_max512", 1.0, 256, 8, 9},
		{"ir2.0s_block256_lat256_max512", 2.0, 256, 8, 9},
		{"ir4.0s_block256_lat256_max512", 4.0, 256, 8, 9},
		{"ir2.0s_block256_lat64_max512", 2.0, 256, 6, 9},
		// Shows how allowing larger max partitions affects throughput for long IRs.
		{"ir2.0s_block256_lat256_max4096", 2.0, 256, 8, 12},
	}

	for _, testCase := range testCases {
		b.Run(testCase.name, func(b *testing.B) {
			irData := generateRealisticIR(sampleRate, testCase.seconds, channels)

			left, err := NewLowLatencyConvolutionEngine(irData[0], testCase.minBlockOrder, testCase.maxBlockOrder)
			if err != nil {
				b.Fatalf("failed to create left engine: %v", err)
			}

			right, err := NewLowLatencyConvolutionEngine(irData[1], testCase.minBlockOrder, testCase.maxBlockOrder)
			if err != nil {
				b.Fatalf("failed to create right engine: %v", err)
			}

			in := generateTestInput(testCase.blockSize)
			outL := make([]float32, testCase.blockSize)
			outR := make([]float32, testCase.blockSize)

			b.ReportAllocs()
			b.SetBytes(int64(testCase.blockSize * channels * 4))
			b.ResetTimer()

			for range b.N {
				err := left.ProcessBlock(in, outL)
				if err != nil {
					b.Fatalf("left ProcessBlock failed: %v", err)
				}

				err = right.ProcessBlock(in, outR)
				if err != nil {
					b.Fatalf("right ProcessBlock failed: %v", err)
				}
			}
		})
	}
}

func BenchmarkRealisticConvolutionReverb_ProcessBlock_Allocations(b *testing.B) {
	// This benchmark intentionally uses ConvolutionReverb.ProcessBlock to surface
	// any per-call allocations in the real callback-ish wrapper.
	const sampleRate = 48000
	const channels = 2
	const seconds = 2.0
	const blockSize = 256

	reverb := NewConvolutionReverb(sampleRate, channels)

	// Set typical config (matches main.go defaults).
	reverb.engineType = EngineTypeLowLatency
	reverb.minBlockOrder = 8 // 256
	reverb.maxBlockOrder = 9 // 512

	irData := generateRealisticIR(sampleRate, seconds, channels)

	reverb.mu.Lock()

	err := reverb.applyImpulseResponseUnlocked(irData, sampleRate)
	if err != nil {
		reverb.mu.Unlock()
		b.Fatalf("failed to apply IR: %v", err)
	}

	reverb.mu.Unlock()

	in := generateTestInput(blockSize)
	outL := make([]float32, blockSize)
	outR := make([]float32, blockSize)

	b.ReportAllocs()
	b.SetBytes(int64(blockSize * channels * 4))
	b.ResetTimer()

	for range b.N {
		reverb.ProcessBlock(in, outL, 0)
		reverb.ProcessBlock(in, outR, 1)
	}
}
