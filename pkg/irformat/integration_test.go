package irformat

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestIntegrationWriteReadFile tests writing to and reading from an actual file.
func TestIntegrationWriteReadFile(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.irlib")

	// Create test library with varied content
	lib := createTestLibrary()

	// Write to file
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	if err := WriteLibrary(file, lib); err != nil {
		file.Close()
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	file.Close()

	// Get file size for verification
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}

	t.Logf("Library file size: %d bytes", info.Size())

	// Read back from file
	file, err = os.Open(filePath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	loadedLib, err := ReadLibrary(file)
	if err != nil {
		t.Fatalf("ReadLibrary failed: %v", err)
	}

	// Verify library contents
	verifyLibrary(t, lib, loadedLib)
}

// TestIntegrationLazyLoading tests that the reader supports lazy loading.
func TestIntegrationLazyLoading(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "lazy.irlib")

	lib := createTestLibrary()

	// Write library
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	if err := WriteLibrary(file, lib); err != nil {
		file.Close()
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	file.Close()

	// Open for reading
	file, err = os.Open(filePath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	reader, err := NewReader(file)
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	// Access index without loading audio
	entries := reader.ListIRs()
	if len(entries) != len(lib.IRs) {
		t.Fatalf("Index entry count mismatch: got %d, want %d", len(entries), len(lib.IRs))
	}

	// Verify index metadata
	for i, entry := range entries {
		expected := lib.IRs[i].Metadata
		if entry.Name != expected.Name {
			t.Errorf("Index entry %d name: got %q, want %q", i, entry.Name, expected.Name)
		}

		if entry.Category != expected.Category {
			t.Errorf("Index entry %d category: got %q, want %q", i, entry.Category, expected.Category)
		}

		if entry.Channels != expected.Channels {
			t.Errorf("Index entry %d channels: got %d, want %d", i, entry.Channels, expected.Channels)
		}

		if entry.Length != expected.Length {
			t.Errorf("Index entry %d length: got %d, want %d", i, entry.Length, expected.Length)
		}
	}

	// Load only specific IR (lazy)
	ir, err := reader.LoadIR(2)
	if err != nil {
		t.Fatalf("LoadIR(2) failed: %v", err)
	}

	if ir.Metadata.Name != lib.IRs[2].Metadata.Name {
		t.Errorf("Loaded IR name: got %q, want %q", ir.Metadata.Name, lib.IRs[2].Metadata.Name)
	}
}

// TestIntegrationIndexSeeking tests that index-based seeking works correctly.
func TestIntegrationIndexSeeking(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "seeking.irlib")

	lib := createTestLibrary()

	// Write library
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	if err := WriteLibrary(file, lib); err != nil {
		file.Close()
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	file.Close()

	// Open for reading
	file, err = os.Open(filePath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	reader, err := NewReader(file)
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	// Load IRs out of order to test seeking
	indices := []int{3, 0, 4, 1, 2}
	for _, idx := range indices {
		ir, err := reader.LoadIR(idx)
		if err != nil {
			t.Fatalf("LoadIR(%d) failed: %v", idx, err)
		}

		expected := lib.IRs[idx]
		if ir.Metadata.Name != expected.Metadata.Name {
			t.Errorf("LoadIR(%d) name: got %q, want %q", idx, ir.Metadata.Name, expected.Metadata.Name)
		}

		verifyAudioDataMatch(t, expected.Audio.Data, ir.Audio.Data)
	}
}

// TestIntegrationVariedContent tests handling of varied IR characteristics.
func TestIntegrationVariedContent(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "varied.irlib")

	// Create library with extreme variations
	lib := &IRLibrary{
		Version: CurrentVersion,
		IRs: []*ImpulseResponse{
			// Very short mono IR
			{
				Metadata: IRMetadata{
					Name:       "Tiny",
					Category:   "Test",
					SampleRate: 22050,
					Channels:   1,
					Length:     10,
				},
				Audio: AudioData{Data: [][]float32{make([]float32, 10)}},
			},
			// Long stereo IR
			{
				Metadata: IRMetadata{
					Name:       "Long Stereo",
					Category:   "Hall",
					SampleRate: 96000,
					Channels:   2,
					Length:     96000, // 1 second at 96kHz
				},
				Audio: AudioData{Data: [][]float32{
					generateSineWave(96000, 440, 96000),
					generateSineWave(96000, 880, 96000),
				}},
			},
			// IR with all metadata fields
			{
				Metadata: IRMetadata{
					Name:        "Full Metadata",
					Description: "This is a test IR with all metadata fields populated for testing purposes.",
					Category:    "Special",
					Tags:        []string{"test", "full", "metadata", "integration"},
					SampleRate:  48000,
					Channels:    1,
					Length:      1000,
				},
				Audio: AudioData{Data: [][]float32{generateSineWave(1000, 1000, 48000)}},
			},
			// High sample rate
			{
				Metadata: IRMetadata{
					Name:       "Hi-Res",
					Category:   "Studio",
					SampleRate: 192000,
					Channels:   2,
					Length:     19200, // 0.1 seconds
				},
				Audio: AudioData{Data: [][]float32{
					generateSineWave(19200, 10000, 192000),
					generateSineWave(19200, 12000, 192000),
				}},
			},
		},
	}

	// Write and read back
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	if err := WriteLibrary(file, lib); err != nil {
		file.Close()
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	file.Close()

	file, err = os.Open(filePath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	loadedLib, err := ReadLibrary(file)
	if err != nil {
		t.Fatalf("ReadLibrary failed: %v", err)
	}

	verifyLibrary(t, lib, loadedLib)
}

// TestIntegrationFileSizeReduction tests that f16 encoding reduces file size.
func TestIntegrationFileSizeReduction(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "size.irlib")

	// Create a reasonably sized IR
	samples := 48000 * 2 // 2 seconds at 48kHz stereo
	lib := &IRLibrary{
		Version: CurrentVersion,
		IRs: []*ImpulseResponse{
			{
				Metadata: IRMetadata{
					Name:       "Size Test",
					SampleRate: 48000,
					Channels:   2,
					Length:     48000,
				},
				Audio: AudioData{Data: [][]float32{
					generateSineWave(48000, 440, 48000),
					generateSineWave(48000, 880, 48000),
				}},
			},
		},
	}

	// Write library
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	if err := WriteLibrary(file, lib); err != nil {
		file.Close()
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	file.Close()

	// Check file size
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}

	// Expected: samples * 2 bytes (f16) + overhead
	// Float32 equivalent would be samples * 4 bytes
	float32Size := samples * 4
	actualSize := int(info.Size())

	// F16 should be roughly 50% of float32
	// Allow for metadata overhead (expect at least 40% reduction)
	maxExpectedSize := int(float64(float32Size) * 0.6)

	t.Logf("Float32 equivalent: %d bytes", float32Size)
	t.Logf("Actual file size: %d bytes", actualSize)
	t.Logf("Reduction: %.1f%%", 100*(1-float64(actualSize)/float64(float32Size)))

	if actualSize > maxExpectedSize {
		t.Errorf("File size too large: got %d, expected less than %d", actualSize, maxExpectedSize)
	}
}

// createTestLibrary creates a test library with varied content.
func createTestLibrary() *IRLibrary {
	return &IRLibrary{
		Version: CurrentVersion,
		IRs: []*ImpulseResponse{
			{
				Metadata: IRMetadata{
					Name:        "Large Hall",
					Description: "A large concert hall reverb",
					Category:    "Hall",
					Tags:        []string{"large", "concert"},
					SampleRate:  48000,
					Channels:    2,
					Length:      48000,
				},
				Audio: AudioData{Data: [][]float32{
					generateDecay(48000),
					generateDecay(48000),
				}},
			},
			{
				Metadata: IRMetadata{
					Name:       "Small Room",
					Category:   "Room",
					SampleRate: 44100,
					Channels:   1,
					Length:     22050,
				},
				Audio: AudioData{Data: [][]float32{generateDecay(22050)}},
			},
			{
				Metadata: IRMetadata{
					Name:       "Plate Reverb",
					Category:   "Plate",
					SampleRate: 48000,
					Channels:   2,
					Length:     96000,
				},
				Audio: AudioData{Data: [][]float32{
					generateDecay(96000),
					generateDecay(96000),
				}},
			},
			{
				Metadata: IRMetadata{
					Name:       "Spring",
					Category:   "Spring",
					SampleRate: 48000,
					Channels:   1,
					Length:     24000,
				},
				Audio: AudioData{Data: [][]float32{generateDecay(24000)}},
			},
			{
				Metadata: IRMetadata{
					Name:       "Cathedral",
					Category:   "Hall",
					Tags:       []string{"large", "church", "reverb"},
					SampleRate: 48000,
					Channels:   2,
					Length:     144000,
				},
				Audio: AudioData{Data: [][]float32{
					generateDecay(144000),
					generateDecay(144000),
				}},
			},
		},
	}
}

// verifyLibrary verifies that two libraries match.
func verifyLibrary(t *testing.T, expected, actual *IRLibrary) {
	t.Helper()

	if len(expected.IRs) != len(actual.IRs) {
		t.Fatalf("IR count mismatch: got %d, want %d", len(actual.IRs), len(expected.IRs))
	}

	for i := range expected.IRs {
		exp := expected.IRs[i]
		act := actual.IRs[i]

		if act.Metadata.Name != exp.Metadata.Name {
			t.Errorf("IR %d name: got %q, want %q", i, act.Metadata.Name, exp.Metadata.Name)
		}

		if act.Metadata.Description != exp.Metadata.Description {
			t.Errorf("IR %d description: got %q, want %q", i, act.Metadata.Description, exp.Metadata.Description)
		}

		if act.Metadata.Category != exp.Metadata.Category {
			t.Errorf("IR %d category: got %q, want %q", i, act.Metadata.Category, exp.Metadata.Category)
		}

		if act.Metadata.SampleRate != exp.Metadata.SampleRate {
			t.Errorf("IR %d sample rate: got %v, want %v", i, act.Metadata.SampleRate, exp.Metadata.SampleRate)
		}

		if act.Metadata.Channels != exp.Metadata.Channels {
			t.Errorf("IR %d channels: got %d, want %d", i, act.Metadata.Channels, exp.Metadata.Channels)
		}

		if act.Metadata.Length != exp.Metadata.Length {
			t.Errorf("IR %d length: got %d, want %d", i, act.Metadata.Length, exp.Metadata.Length)
		}

		if len(act.Metadata.Tags) != len(exp.Metadata.Tags) {
			t.Errorf("IR %d tag count: got %d, want %d", i, len(act.Metadata.Tags), len(exp.Metadata.Tags))
		}

		verifyAudioDataMatch(t, exp.Audio.Data, act.Audio.Data)
	}
}

// verifyAudioDataMatch verifies audio data within f16 tolerance.
func verifyAudioDataMatch(t *testing.T, expected, actual [][]float32) {
	t.Helper()

	if len(expected) != len(actual) {
		t.Errorf("Channel count mismatch: got %d, want %d", len(actual), len(expected))
		return
	}

	for ch := range expected {
		if len(expected[ch]) != len(actual[ch]) {
			t.Errorf("Channel %d length mismatch: got %d, want %d", ch, len(actual[ch]), len(expected[ch]))
			continue
		}

		for i := range expected[ch] {
			exp := expected[ch][i]
			act := actual[ch][i]

			absErr := math.Abs(float64(exp - act))

			relErr := float64(0)
			if math.Abs(float64(exp)) > 1e-6 {
				relErr = absErr / math.Abs(float64(exp))
			}

			if relErr > 0.01 && absErr > 1e-4 {
				t.Errorf("Channel %d sample %d: got %v, want %v", ch, i, act, exp)
				return // Don't spam errors
			}
		}
	}
}

// generateDecay generates an exponential decay signal (typical IR shape).
func generateDecay(n int) []float32 {
	samples := make([]float32, n)
	for i := range n {
		t := float64(i) / float64(n)
		samples[i] = float32(math.Exp(-5 * t))
	}

	return samples
}

// generateSineWave generates a sine wave signal.
func generateSineWave(n int, freq, sampleRate float64) []float32 {
	samples := make([]float32, n)
	for i := range n {
		t := float64(i) / sampleRate
		samples[i] = float32(math.Sin(2 * math.Pi * freq * t))
	}

	return samples
}

// Ensure memFile is used (imported from unit tests).
var _ io.ReadWriteSeeker = (*memFile)(nil)
