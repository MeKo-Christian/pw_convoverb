package dsp

import (
	"io"
	"math"
	"testing"

	algofft "github.com/MeKo-Christian/algo-fft"
	"pw-convoverb/pkg/irformat"
)

// memFile is an in-memory file that supports io.ReadWriteSeeker for testing.
type memFile struct {
	data []byte
	pos  int64
}

func newMemFile() *memFile {
	return &memFile{data: make([]byte, 0)}
}

func (m *memFile) Write(p []byte) (n int, err error) {
	needed := int(m.pos) + len(p)
	if needed > len(m.data) {
		newData := make([]byte, needed)
		copy(newData, m.data)
		m.data = newData
	}

	copy(m.data[m.pos:], p)
	m.pos += int64(len(p))

	return len(p), nil
}

func (m *memFile) Read(p []byte) (n int, err error) {
	if m.pos >= int64(len(m.data)) {
		return 0, io.EOF
	}

	n = copy(p, m.data[m.pos:])
	m.pos += int64(n)

	return n, nil
}

func (m *memFile) Seek(offset int64, whence int) (int64, error) {
	var newPos int64

	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = m.pos + offset
	case io.SeekEnd:
		newPos = int64(len(m.data)) + offset
	}

	if newPos < 0 {
		return 0, io.EOF
	}

	m.pos = newPos

	return m.pos, nil
}

func TestNewConvolutionReverb(t *testing.T) {
	t.Parallel()
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
	t.Parallel()

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
	t.Parallel()

	reverb := NewConvolutionReverb(48000, 2)

	// Without loaded IR, output should equal input
	input := float32(0.5)
	output := reverb.ProcessSample(input, 0)

	if output != input {
		t.Errorf("Expected output to equal input when IR not loaded, got %f != %f", output, input)
	}
}

func TestProcessBlock(t *testing.T) {
	t.Parallel()

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

	for range b.N {
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

	for range b.N {
		reverb.ProcessBlock(input, output, 0)
	}
}

func TestOverlapAddEngine(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()

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

// Helper function for testing.
func absComplexFloat32(c complex64) float32 {
	r := real(c)
	i := imag(c)

	return float32(math.Sqrt(float64(r*r + i*i)))
}

// TestLoadImpulseResponseFromLibrary tests loading an IR from an in-memory library.
func TestLoadImpulseResponseFromLibrary(t *testing.T) {
	t.Parallel()
	// Create a test IR library in memory
	lib := irformat.NewIRLibrary()

	// Add a test IR with a simple exponential decay
	irLength := 1024

	irData := make([][]float32, 2) // stereo
	for ch := range 2 {
		irData[ch] = make([]float32, irLength)
		for i := range irLength {
			irData[ch][i] = float32(0.8 * math.Exp(-3.0*float64(i)/float64(irLength)))
		}
	}

	ir := irformat.NewImpulseResponse("Test IR", 48000, 2, irData)
	ir.Metadata.Category = "Test"
	lib.AddIR(ir)

	// Write library to buffer
	buf := newMemFile()

	err := irformat.WriteLibrary(buf, lib)
	if err != nil {
		t.Fatalf("Failed to write library: %v", err)
	}

	// Read back and verify
	_, err = buf.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Failed to seek: %v", err)
	}

	reader, err := irformat.NewReader(buf)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	if reader.IRCount() != 1 {
		t.Fatalf("Expected 1 IR, got %d", reader.IRCount())
	}

	// Test ListLibraryIRs function (requires a file, so we skip that)

	// Create reverb and apply the IR directly
	reverb := NewConvolutionReverb(48000, 2)

	// Load IR from the library
	loadedIR, err := reader.LoadIR(0)
	if err != nil {
		t.Fatalf("Failed to load IR: %v", err)
	}

	// Apply to reverb using the internal method
	err = reverb.applyImpulseResponse(loadedIR.Audio.Data, loadedIR.Metadata.SampleRate)
	if err != nil {
		t.Fatalf("Failed to apply impulse response: %v", err)
	}

	// Verify reverb is enabled
	if !reverb.enabled {
		t.Error("Reverb should be enabled after loading IR")
	}

	// Test processing a block
	input := make([]float32, 64)
	output := make([]float32, 64)

	for i := range input {
		input[i] = 0.5
	}

	reverb.ProcessBlock(input, output, 0)

	// Verify output has some signal
	hasNonZero := false

	for _, s := range output {
		if s != 0 {
			hasNonZero = true
			break
		}
	}

	if !hasNonZero {
		t.Error("Output should have non-zero samples after processing")
	}
}

// TestLoadIRByNameDSP tests loading an IR by name from a library.
func TestLoadIRByNameDSP(t *testing.T) {
	t.Parallel()
	// Create a test library with multiple IRs
	lib := irformat.NewIRLibrary()

	names := []string{"Small Room", "Large Hall", "Plate"}
	for _, name := range names {
		irData := make([][]float32, 1) // mono

		irData[0] = make([]float32, 512)
		for i := range 512 {
			irData[0][i] = float32(0.5 * math.Exp(-2.0*float64(i)/512.0))
		}

		ir := irformat.NewImpulseResponse(name, 48000, 1, irData)
		lib.AddIR(ir)
	}

	// Write library to buffer
	buf := newMemFile()

	err := irformat.WriteLibrary(buf, lib)
	if err != nil {
		t.Fatalf("Failed to write library: %v", err)
	}

	// Read back
	_, err = buf.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Failed to seek: %v", err)
	}

	reader, err := irformat.NewReader(buf)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// Load by name
	ir, err := reader.LoadIRByName("Large Hall")
	if err != nil {
		t.Fatalf("Failed to load IR by name: %v", err)
	}

	if ir.Metadata.Name != "Large Hall" {
		t.Errorf("Expected name 'Large Hall', got %q", ir.Metadata.Name)
	}

	// Test loading non-existent name
	_, err = reader.LoadIRByName("Non-existent")
	if err == nil {
		t.Error("Expected error when loading non-existent IR")
	}
}

// TestLoadImpulseResponseFromBytes tests loading an IR from embedded byte data.
func TestLoadImpulseResponseFromBytes(t *testing.T) {
	t.Parallel()
	// Create a test library
	lib := irformat.NewIRLibrary()

	irData := make([][]float32, 2)
	for ch := range 2 {
		irData[ch] = make([]float32, 512)
		for i := range 512 {
			irData[ch][i] = float32(0.6 * math.Exp(-2.0*float64(i)/512.0))
		}
	}

	ir := irformat.NewImpulseResponse("Embedded Test", 48000, 2, irData)
	lib.AddIR(ir)

	// Write to buffer
	buf := newMemFile()

	err := irformat.WriteLibrary(buf, lib)
	if err != nil {
		t.Fatalf("Failed to write library: %v", err)
	}

	// Get bytes
	embeddedData := buf.data

	// Create reverb and load from bytes
	reverb := NewConvolutionReverb(48000, 2)

	err = reverb.LoadImpulseResponseFromBytes(embeddedData, "", 0)
	if err != nil {
		t.Fatalf("Failed to load IR from bytes: %v", err)
	}

	if !reverb.enabled {
		t.Error("Reverb should be enabled after loading IR")
	}

	// Test loading by name
	reverb2 := NewConvolutionReverb(48000, 2)

	err = reverb2.LoadImpulseResponseFromBytes(embeddedData, "Embedded Test", 0)
	if err != nil {
		t.Fatalf("Failed to load IR by name from bytes: %v", err)
	}

	if !reverb2.enabled {
		t.Error("Reverb should be enabled after loading IR by name")
	}
}

// TestApplyImpulseResponseChannelMismatch tests handling of channel count mismatch.
func TestApplyImpulseResponseChannelMismatch(t *testing.T) {
	t.Parallel()
	// Create a mono IR
	irData := make([][]float32, 1)

	irData[0] = make([]float32, 256)
	for i := range 256 {
		irData[0][i] = float32(0.7 * math.Exp(-2.0*float64(i)/256.0))
	}

	// Create stereo reverb
	reverb := NewConvolutionReverb(48000, 2)

	// Apply mono IR to stereo reverb - should duplicate mono to both channels
	err := reverb.applyImpulseResponse(irData, 48000)
	if err != nil {
		t.Fatalf("Failed to apply mono IR to stereo reverb: %v", err)
	}

	if !reverb.enabled {
		t.Error("Reverb should be enabled")
	}

	// Both channels should have engines
	if reverb.engines[0] == nil || reverb.engines[1] == nil {
		t.Error("Both channels should have engines")
	}
}
