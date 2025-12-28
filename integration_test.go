package main

import (
	"testing"

	"pw-convoverb/dsp"
)

// TestIntegrationReverbProcessing tests the full audio processing pipeline.
func TestIntegrationReverbProcessing(t *testing.T) {
	t.Parallel()
	const sampleRate = 48000.0
	const channels = 2

	// Create reverb instance
	reverb = dsp.NewConvolutionReverb(sampleRate, channels)
	if reverb == nil {
		t.Fatal("Failed to create reverb instance")
	}

	// Load synthetic IR
	err := reverb.LoadImpulseResponse("")
	if err != nil {
		t.Fatalf("Failed to load impulse response: %v", err)
	}

	// Configure parameters
	reverb.SetWetLevel(0.3)
	reverb.SetDryLevel(0.7)

	// Create test signal (simple sine-like pattern)
	const blockSize = 128
	testSignal := make([]float32, blockSize*channels)

	for i := range blockSize {
		for ch := range channels {
			testSignal[i*channels+ch] = 0.5
		}
	}

	// Process through reverb
	processAudioBuffer(testSignal)

	// Verify output is not all zeros
	allZeros := true

	for _, sample := range testSignal {
		if sample != 0.0 {
			allZeros = false
			break
		}
	}

	if allZeros {
		t.Error("Processed audio is all zeros")
	}
}

// TestIntegrationStereoIndependence verifies that channels are processed independently.
func TestIntegrationStereoIndependence(t *testing.T) {
	t.Parallel()
	const sampleRate = 48000.0
	const channels = 2

	reverb = dsp.NewConvolutionReverb(sampleRate, channels)
	_ = reverb.LoadImpulseResponse("")

	const blockSize = 64
	testSignal := make([]float32, blockSize*channels)

	// Set different signals for each channel
	for i := range blockSize {
		testSignal[i*channels+0] = 0.8 // Left channel
		testSignal[i*channels+1] = 0.2 // Right channel
	}

	processAudioBuffer(testSignal)

	// Channels should still have different characteristics
	// (This is a basic check - more sophisticated tests would verify actual independence)
	leftSum := float32(0.0)
	rightSum := float32(0.0)

	for i := range blockSize {
		leftSum += testSignal[i*channels+0]
		rightSum += testSignal[i*channels+1]
	}

	// Verify that channels are not identical
	if leftSum == rightSum {
		t.Error("Left and right channels appear identical after processing")
	}
}

func BenchmarkIntegrationProcessing(b *testing.B) {
	const sampleRate = 48000.0
	const channels = 2

	reverb = dsp.NewConvolutionReverb(sampleRate, channels)
	_ = reverb.LoadImpulseResponse("")

	const blockSize = 512
	testSignal := make([]float32, blockSize*channels)

	for i := range testSignal {
		testSignal[i] = 0.5
	}

	b.ResetTimer()

	for range b.N {
		processAudioBuffer(testSignal)
	}
}
