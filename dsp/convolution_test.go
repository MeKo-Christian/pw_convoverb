package dsp

import (
	"math"
	"testing"

	"github.com/MeKo-Christian/algo-fft"
)

func TestNewConvolutionReverb(t *testing.T) {
	const sampleRate = 48000.0
	const channels = 2

	reverb := NewConvolutionReverb(sampleRate, channels)
	if reverb == nil {
		t.Fatal("NewConvolutionReverb returned nil")
	}

	if reverb.sampleRate != sampleRate {
		t.Errorf("Expected sample rate %f, got %f", sampleRate, reverb.sampleRate)
	}

	if reverb.channels != channels {
		t.Errorf("Expected %d channels, got %d", channels, reverb.channels)
	}
}

func TestSetWetDryLevels(t *testing.T) {
	reverb := NewConvolutionReverb(48000, 2)

	// Test wet level
	reverb.SetWetLevel(0.5)
	if got := reverb.GetWetLevel(); got != 0.5 {
		t.Errorf("Expected wet level 0.5, got %f", got)
	}

	// Test clamping
	reverb.SetWetLevel(1.5)
	if got := reverb.GetWetLevel(); got != 1.0 {
		t.Errorf("Expected wet level clamped to 1.0, got %f", got)
	}

	reverb.SetWetLevel(-0.5)
	if got := reverb.GetWetLevel(); got != 0.0 {
		t.Errorf("Expected wet level clamped to 0.0, got %f", got)
	}

	// Test dry level
	reverb.SetDryLevel(0.7)
	if got := reverb.GetDryLevel(); got != 0.7 {
		t.Errorf("Expected dry level 0.7, got %f", got)
	}
}

func TestProcessSampleWithoutIR(t *testing.T) {
	reverb := NewConvolutionReverb(48000, 2)

	// Without loaded IR, output should equal input
	input := float32(0.5)
	output := reverb.ProcessSample(input, 0)

	if output != input {
		t.Errorf("Expected output to equal input when IR not loaded, got %f != %f", output, input)
	}
}

func TestProcessBlock(t *testing.T) {
	reverb := NewConvolutionReverb(48000, 2)

	const blockSize = 64
	input := make([]float32, blockSize)
	output := make([]float32, blockSize)

	// Fill input with test signal
	for i := range input {
		input[i] = 0.5
	}

	// Process block
	reverb.ProcessBlock(input, output, 0)

	// Check output is not all zeros (basic sanity check)
	allZeros := true
	for _, sample := range output {
		if sample != 0.0 {
			allZeros = false
			break
		}
	}

	if allZeros {
		t.Error("Output is all zeros")
	}
}

func BenchmarkProcessSample(b *testing.B) {
	reverb := NewConvolutionReverb(48000, 2)
	_ = reverb.LoadImpulseResponse("") // Load synthetic IR

	input := float32(0.5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = reverb.ProcessSample(input, 0)
	}
}

func BenchmarkProcessBlock(b *testing.B) {
	reverb := NewConvolutionReverb(48000, 2)
	_ = reverb.LoadImpulseResponse("") // Load synthetic IR

	const blockSize = 512
	input := make([]float32, blockSize)
	output := make([]float32, blockSize)

	for i := range input {
		input[i] = 0.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reverb.ProcessBlock(input, output, 0)
	}
}

func TestOverlapAddEngine(t *testing.T) {
	// Create a simple impulse response (short to avoid slow FFT)
	irLength := 16
	ir := make([]float32, irLength)
	for i := range irLength {
		ir[i] = float32(0.9 * math.Pow(0.95, float64(i)))
	}

	// Create engine with 8-sample blocks
	engine := NewOverlapAddEngine(ir, 8)

	if engine == nil {
		t.Fatal("NewOverlapAddEngine returned nil")
	}

	if engine.irLen != irLength {
		t.Errorf("Expected IR length %d, got %d", irLength, engine.irLen)
	}

	if engine.blockSize != 8 {
		t.Errorf("Expected block size 8, got %d", engine.blockSize)
	}
}

func TestOverlapAddProcessing(t *testing.T) {
	// Create impulse response
	ir := []float32{0.5, 0.3, 0.1, 0.05}

	// Create engine
	engine := NewOverlapAddEngine(ir, 4)

	// Create input block (impulse)
	input := []float32{1.0, 0.0, 0.0, 0.0}

	// Process block
	output := engine.ProcessBlock(input)

	// Output should have at least the first sample non-zero
	if len(output) == 0 {
		t.Fatal("Output is empty")
	}

	if output[0] == 0 {
		t.Error("First output sample should be non-zero for impulse input")
	}
}

func TestOverlapAddConsistency(t *testing.T) {
	// Create a simple IR
	ir := []float32{0.7, 0.2, 0.1}

	// Create engine
	engine := NewOverlapAddEngine(ir, 2)

	// Process two blocks separately
	block1 := []float32{1.0, 0.5}
	block2 := []float32{0.3, 0.2}

	out1 := engine.ProcessBlock(block1)
	out2 := engine.ProcessBlock(block2)

	if len(out1) != len(block1) {
		t.Errorf("Expected output length %d, got %d", len(block1), len(out1))
	}

	if len(out2) != len(block2) {
		t.Errorf("Expected output length %d, got %d", len(block2), len(out2))
	}

	// Both should be non-zero (basic sanity)
	hasNonZero1 := false
	for _, s := range out1 {
		if s != 0 {
			hasNonZero1 = true
			break
		}
	}

	hasNonZero2 := false
	for _, s := range out2 {
		if s != 0 {
			hasNonZero2 = true
			break
		}
	}

	if !hasNonZero1 || !hasNonZero2 {
		t.Error("Output should contain non-zero samples")
	}
}

func TestFFTRoundtrip(t *testing.T) {
	// Test that FFT -> IFFT gives back original (within floating point precision)
	// Use power-of-2 size for correct FFT behavior
	input := []complex64{
		complex(1, 0),
		complex(2, 1),
		complex(3, -1),
		complex(0, 2),
	}

	// Create FFT plan
	plan, err := algofft.NewPlan32(len(input))
	if err != nil {
		t.Fatalf("failed to create FFT plan: %v", err)
	}

	// Make copy for FFT
	fftResult := make([]complex64, len(input))
	copy(fftResult, input)

	// Forward FFT
	err = plan.Forward(fftResult, fftResult)
	if err != nil {
		t.Fatalf("forward FFT failed: %v", err)
	}

	// Inverse FFT (algo-fft scales by 1/N automatically)
	err = plan.Inverse(fftResult, fftResult)
	if err != nil {
		t.Fatalf("inverse FFT failed: %v", err)
	}

	// Check results
	tolerance := float32(1e-4)
	for i, orig := range input {
		if absComplexFloat32(fftResult[i]-orig) > tolerance {
			t.Errorf("Index %d: expected %v, got %v (diff: %v)", i, orig, fftResult[i], fftResult[i]-orig)
		}
	}
}

func TestPowerOf2(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{16, 16},
		{17, 32},
		{1000, 1024},
	}

	for _, tt := range tests {
		got := nextPowerOf2(tt.input)
		if got != tt.expected {
			t.Errorf("nextPowerOf2(%d) = %d, expected %d", tt.input, got, tt.expected)
		}
	}
}

// Helper function for testing
func absComplexFloat32(c complex64) float32 {
	r := real(c)
	i := imag(c)
	return float32(math.Sqrt(float64(r*r + i*i)))
}
