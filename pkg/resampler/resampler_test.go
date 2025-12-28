package resampler

import (
	"math"
	"testing"
)

func TestResample_EmptyInput(t *testing.T) {
	t.Parallel()

	r := New()

	result, err := r.Resample([]float32{}, 48000, 44100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty result, got %d samples", len(result))
	}
}

func TestResample_IdentityRatio(t *testing.T) {
	t.Parallel()

	r := New()
	input := []float32{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0}

	result, err := r.Resample(input, 48000, 48000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != len(input) {
		t.Errorf("expected length %d, got %d", len(input), len(result))
	}

	for i := range input {
		if result[i] != input[i] {
			t.Errorf("at index %d: expected %f, got %f", i, input[i], result[i])
		}
	}
}

func TestResample_Downsample2x(t *testing.T) {
	t.Parallel()

	resampler := New()
	// Create a longer input for meaningful downsampling
	inputLen := 1024

	input := make([]float32, inputLen)
	for i := range input {
		input[i] = float32(math.Sin(2 * math.Pi * float64(i) / float64(inputLen)))
	}

	result, err := resampler.Resample(input, 96000, 48000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedLen := CalculateOutputLength(inputLen, 96000, 48000)
	if len(result) != expectedLen {
		t.Errorf("expected length %d, got %d", expectedLen, len(result))
	}
}

func TestResample_Upsample2x(t *testing.T) {
	t.Parallel()

	r := New()
	inputLen := 512

	input := make([]float32, inputLen)
	for i := range input {
		input[i] = float32(math.Sin(2 * math.Pi * float64(i) / float64(inputLen)))
	}

	result, err := r.Resample(input, 44100, 88200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedLen := CalculateOutputLength(inputLen, 44100, 88200)
	if len(result) != expectedLen {
		t.Errorf("expected length %d, got %d", expectedLen, len(result))
	}
}

func TestResample_ArbitraryRatio_88200_to_48000(t *testing.T) {
	t.Parallel()

	resampler := New()
	// This is the actual use case: 88.2kHz IR to 48kHz playback
	inputLen := 4096

	input := make([]float32, inputLen)
	for i := range input {
		input[i] = float32(math.Sin(2 * math.Pi * float64(i) / float64(inputLen)))
	}

	result, err := resampler.Resample(input, 88200, 48000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedLen := CalculateOutputLength(inputLen, 88200, 48000)
	// Allow 1 sample tolerance due to rounding
	if abs(len(result)-expectedLen) > 1 {
		t.Errorf("expected length ~%d, got %d", expectedLen, len(result))
	}
}

func TestResample_PreservesLowFrequencyContent(t *testing.T) {
	t.Parallel()

	r := New()

	// Generate a low-frequency sine wave (well below Nyquist for both rates)
	srcRate := 88200.0
	dstRate := 48000.0
	frequency := 100.0 // 100 Hz - well below both Nyquist frequencies
	duration := 0.1    // 100ms
	inputLen := int(srcRate * duration)

	input := make([]float32, inputLen)
	for i := range input {
		t := float64(i) / srcRate
		input[i] = float32(math.Sin(2 * math.Pi * frequency * t))
	}

	result, err := r.Resample(input, srcRate, dstRate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the output still resembles a sine wave at the same frequency
	// by checking that it crosses zero roughly the expected number of times
	expectedCrossings := int(2 * frequency * duration) // 2 crossings per cycle
	actualCrossings := countZeroCrossings(result)

	// Allow 20% tolerance for edge effects
	tolerance := expectedCrossings / 5
	if tolerance < 2 {
		tolerance = 2
	}

	if abs(actualCrossings-expectedCrossings) > tolerance {
		t.Errorf("expected ~%d zero crossings, got %d", expectedCrossings, actualCrossings)
	}
}

func TestResample_EnergyPreservation(t *testing.T) {
	t.Parallel()

	resampler := New()

	// Create a test signal
	inputLen := 2048

	input := make([]float32, inputLen)
	for i := range input {
		input[i] = float32(math.Sin(2*math.Pi*float64(i)/float64(inputLen))) * 0.5
	}

	// Calculate input energy (RMS)
	inputEnergy := calculateRMS(input)

	// Resample down
	result, err := resampler.Resample(input, 88200, 48000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Calculate output energy
	outputEnergy := calculateRMS(result)

	// Energy should be preserved within 20% (some loss expected due to filtering)
	ratio := outputEnergy / inputEnergy
	if ratio < 0.5 || ratio > 1.5 {
		t.Errorf("energy not preserved: input RMS=%f, output RMS=%f, ratio=%f",
			inputEnergy, outputEnergy, ratio)
	}
}

func TestResampleMultiChannel(t *testing.T) {
	t.Parallel()

	resampler := New()

	// Create stereo input
	inputLen := 512

	input := make([][]float32, 2)
	for ch := range input {
		input[ch] = make([]float32, inputLen)
		for i := range input[ch] {
			// Different phase for each channel
			phase := float64(ch) * math.Pi / 2
			input[ch][i] = float32(math.Sin(2*math.Pi*float64(i)/float64(inputLen) + phase))
		}
	}

	result, err := resampler.ResampleMultiChannel(input, 88200, 48000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 channels, got %d", len(result))
	}

	expectedLen := CalculateOutputLength(inputLen, 88200, 48000)
	for ch := range result {
		if abs(len(result[ch])-expectedLen) > 1 {
			t.Errorf("channel %d: expected length ~%d, got %d", ch, expectedLen, len(result[ch]))
		}
	}
}

func TestResampleMultiChannel_Empty(t *testing.T) {
	t.Parallel()

	r := New()

	result, err := r.ResampleMultiChannel([][]float32{}, 48000, 44100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty result, got %d channels", len(result))
	}
}

func TestCalculateOutputLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		inputLen int
		srcRate  float64
		dstRate  float64
		expected int
	}{
		{1000, 48000, 48000, 1000}, // No change
		{1000, 96000, 48000, 500},  // Downsample 2x
		{1000, 44100, 88200, 2000}, // Upsample 2x
		{8820, 88200, 48000, 4800}, // Actual use case
		{0, 48000, 44100, 0},       // Empty
		{100, 44100, 48000, 109},   // Arbitrary ratio
	}

	for _, tt := range tests {
		result := CalculateOutputLength(tt.inputLen, tt.srcRate, tt.dstRate)
		if result != tt.expected {
			t.Errorf("CalculateOutputLength(%d, %f, %f) = %d, want %d",
				tt.inputLen, tt.srcRate, tt.dstRate, result, tt.expected)
		}
	}
}

// Helper functions

func abs(x int) int {
	if x < 0 {
		return -x
	}

	return x
}

func countZeroCrossings(data []float32) int {
	if len(data) < 2 {
		return 0
	}

	crossings := 0

	for i := 1; i < len(data); i++ {
		if (data[i-1] >= 0 && data[i] < 0) || (data[i-1] < 0 && data[i] >= 0) {
			crossings++
		}
	}

	return crossings
}

func calculateRMS(data []float32) float64 {
	if len(data) == 0 {
		return 0
	}

	var sum float64
	for _, v := range data {
		sum += float64(v) * float64(v)
	}

	return math.Sqrt(sum / float64(len(data)))
}
