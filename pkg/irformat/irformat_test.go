package irformat

import (
	"errors"
	"io"
	"math"
	"testing"
)

// memFile is an in-memory file that supports io.ReadWriteSeeker.
type memFile struct {
	data []byte
	pos  int64
}

func newMemFile() *memFile {
	return &memFile{data: make([]byte, 0)}
}

func (m *memFile) Write(p []byte) (n int, err error) {
	// Grow the buffer if needed
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

func (m *memFile) Bytes() []byte {
	return m.data
}

// TestWriteReadSingleIR tests writing and reading a single IR.
func TestWriteReadSingleIR(t *testing.T) {
	// Create test IR
	ir := &ImpulseResponse{
		Metadata: IRMetadata{
			Name:        "Test IR",
			Description: "A test impulse response",
			Category:    "Test",
			Tags:        []string{"mono", "test"},
			SampleRate:  48000,
			Channels:    1,
			Length:      100,
		},
		Audio: AudioData{
			Data: [][]float32{generateTestSamples(100)},
		},
	}

	// Write to buffer
	buf := newMemFile()
	writer := NewWriter(buf)

	if err := writer.WriteHeader(1); err != nil {
		t.Fatalf("WriteHeader failed: %v", err)
	}

	if err := writer.WriteIR(ir); err != nil {
		t.Fatalf("WriteIR failed: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read back
	buf.Seek(0, io.SeekStart)

	reader, err := NewReader(buf)
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	if reader.IRCount() != 1 {
		t.Errorf("expected 1 IR, got %d", reader.IRCount())
	}

	loadedIR, err := reader.LoadIR(0)
	if err != nil {
		t.Fatalf("LoadIR failed: %v", err)
	}

	// Verify metadata
	if loadedIR.Metadata.Name != ir.Metadata.Name {
		t.Errorf("name mismatch: got %q, want %q", loadedIR.Metadata.Name, ir.Metadata.Name)
	}

	if loadedIR.Metadata.Description != ir.Metadata.Description {
		t.Errorf("description mismatch: got %q, want %q", loadedIR.Metadata.Description, ir.Metadata.Description)
	}

	if loadedIR.Metadata.Category != ir.Metadata.Category {
		t.Errorf("category mismatch: got %q, want %q", loadedIR.Metadata.Category, ir.Metadata.Category)
	}

	if loadedIR.Metadata.SampleRate != ir.Metadata.SampleRate {
		t.Errorf("sample rate mismatch: got %v, want %v", loadedIR.Metadata.SampleRate, ir.Metadata.SampleRate)
	}

	if loadedIR.Metadata.Channels != ir.Metadata.Channels {
		t.Errorf("channels mismatch: got %d, want %d", loadedIR.Metadata.Channels, ir.Metadata.Channels)
	}

	if loadedIR.Metadata.Length != ir.Metadata.Length {
		t.Errorf("length mismatch: got %d, want %d", loadedIR.Metadata.Length, ir.Metadata.Length)
	}

	if len(loadedIR.Metadata.Tags) != len(ir.Metadata.Tags) {
		t.Errorf("tags count mismatch: got %d, want %d", len(loadedIR.Metadata.Tags), len(ir.Metadata.Tags))
	}

	// Verify audio data (with f16 precision tolerance)
	verifyAudioData(t, ir.Audio.Data, loadedIR.Audio.Data)
}

// TestWriteReadMultipleIRs tests writing and reading multiple IRs.
func TestWriteReadMultipleIRs(t *testing.T) {
	irs := []*ImpulseResponse{
		{
			Metadata: IRMetadata{
				Name:       "IR 1",
				Category:   "Hall",
				SampleRate: 44100,
				Channels:   1,
				Length:     50,
			},
			Audio: AudioData{Data: [][]float32{generateTestSamples(50)}},
		},
		{
			Metadata: IRMetadata{
				Name:       "IR 2",
				Category:   "Room",
				SampleRate: 48000,
				Channels:   2,
				Length:     100,
			},
			Audio: AudioData{Data: [][]float32{generateTestSamples(100), generateTestSamples(100)}},
		},
		{
			Metadata: IRMetadata{
				Name:       "IR 3",
				Category:   "Plate",
				SampleRate: 96000,
				Channels:   1,
				Length:     200,
			},
			Audio: AudioData{Data: [][]float32{generateTestSamples(200)}},
		},
	}

	// Write library
	lib := NewIRLibrary()
	for _, ir := range irs {
		lib.AddIR(ir)
	}

	buf := newMemFile()
	if err := WriteLibrary(buf, lib); err != nil {
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	// Read back
	buf.Seek(0, io.SeekStart)

	loadedLib, err := ReadLibrary(buf)
	if err != nil {
		t.Fatalf("ReadLibrary failed: %v", err)
	}

	if len(loadedLib.IRs) != len(irs) {
		t.Fatalf("IR count mismatch: got %d, want %d", len(loadedLib.IRs), len(irs))
	}

	for i, ir := range irs {
		loadedIR := loadedLib.IRs[i]
		if loadedIR.Metadata.Name != ir.Metadata.Name {
			t.Errorf("IR %d name mismatch: got %q, want %q", i, loadedIR.Metadata.Name, ir.Metadata.Name)
		}

		if loadedIR.Metadata.Category != ir.Metadata.Category {
			t.Errorf("IR %d category mismatch: got %q, want %q", i, loadedIR.Metadata.Category, ir.Metadata.Category)
		}

		if loadedIR.Metadata.Channels != ir.Metadata.Channels {
			t.Errorf("IR %d channels mismatch: got %d, want %d", i, loadedIR.Metadata.Channels, ir.Metadata.Channels)
		}

		verifyAudioData(t, ir.Audio.Data, loadedIR.Audio.Data)
	}
}

// TestListIRs tests the index-based listing.
func TestListIRs(t *testing.T) {
	lib := NewIRLibrary()
	lib.AddIR(&ImpulseResponse{
		Metadata: IRMetadata{
			Name:       "Hall A",
			Category:   "Hall",
			SampleRate: 48000,
			Channels:   2,
			Length:     1000,
		},
		Audio: AudioData{Data: [][]float32{generateTestSamples(1000), generateTestSamples(1000)}},
	})
	lib.AddIR(&ImpulseResponse{
		Metadata: IRMetadata{
			Name:       "Room B",
			Category:   "Room",
			SampleRate: 44100,
			Channels:   1,
			Length:     500,
		},
		Audio: AudioData{Data: [][]float32{generateTestSamples(500)}},
	})

	buf := newMemFile()
	if err := WriteLibrary(buf, lib); err != nil {
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	buf.Seek(0, io.SeekStart)

	reader, err := NewReader(buf)
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	entries := reader.ListIRs()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Name != "Hall A" {
		t.Errorf("entry 0 name: got %q, want %q", entries[0].Name, "Hall A")
	}

	if entries[0].Category != "Hall" {
		t.Errorf("entry 0 category: got %q, want %q", entries[0].Category, "Hall")
	}

	if entries[0].Channels != 2 {
		t.Errorf("entry 0 channels: got %d, want %d", entries[0].Channels, 2)
	}

	if entries[1].Name != "Room B" {
		t.Errorf("entry 1 name: got %q, want %q", entries[1].Name, "Room B")
	}
}

// TestLoadIRByName tests loading an IR by name.
func TestLoadIRByName(t *testing.T) {
	lib := NewIRLibrary()
	lib.AddIR(&ImpulseResponse{
		Metadata: IRMetadata{Name: "First", SampleRate: 48000, Channels: 1, Length: 10},
		Audio:    AudioData{Data: [][]float32{generateTestSamples(10)}},
	})
	lib.AddIR(&ImpulseResponse{
		Metadata: IRMetadata{Name: "Second", SampleRate: 48000, Channels: 1, Length: 20},
		Audio:    AudioData{Data: [][]float32{generateTestSamples(20)}},
	})
	lib.AddIR(&ImpulseResponse{
		Metadata: IRMetadata{Name: "Third", SampleRate: 48000, Channels: 1, Length: 30},
		Audio:    AudioData{Data: [][]float32{generateTestSamples(30)}},
	})

	buf := newMemFile()
	if err := WriteLibrary(buf, lib); err != nil {
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	buf.Seek(0, io.SeekStart)

	reader, err := NewReader(buf)
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	// Load by name
	ir, err := reader.LoadIRByName("Second")
	if err != nil {
		t.Fatalf("LoadIRByName failed: %v", err)
	}

	if ir.Metadata.Name != "Second" {
		t.Errorf("got name %q, want %q", ir.Metadata.Name, "Second")
	}

	if ir.Metadata.Length != 20 {
		t.Errorf("got length %d, want %d", ir.Metadata.Length, 20)
	}

	// Test not found
	_, err = reader.LoadIRByName("NonExistent")
	if !errors.Is(err, ErrIRNotFound) {
		t.Errorf("expected ErrIRNotFound, got %v", err)
	}
}

// TestInvalidMagic tests that an invalid magic number is rejected.
func TestInvalidMagic(t *testing.T) {
	buf := newMemFile()
	buf.Write([]byte("XXXX")) // Invalid magic
	buf.Seek(0, io.SeekStart)

	_, err := NewReader(buf)
	if !errors.Is(err, ErrInvalidMagic) {
		t.Errorf("expected ErrInvalidMagic, got %v", err)
	}
}

// TestInvalidIndex tests that an invalid index is rejected.
func TestInvalidIndex(t *testing.T) {
	lib := NewIRLibrary()
	lib.AddIR(&ImpulseResponse{
		Metadata: IRMetadata{Name: "Only", SampleRate: 48000, Channels: 1, Length: 10},
		Audio:    AudioData{Data: [][]float32{generateTestSamples(10)}},
	})

	buf := newMemFile()
	if err := WriteLibrary(buf, lib); err != nil {
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	buf.Seek(0, io.SeekStart)

	reader, err := NewReader(buf)
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	_, err = reader.LoadIR(-1)
	if !errors.Is(err, ErrInvalidIndex) {
		t.Errorf("expected ErrInvalidIndex for -1, got %v", err)
	}

	_, err = reader.LoadIR(1)
	if !errors.Is(err, ErrInvalidIndex) {
		t.Errorf("expected ErrInvalidIndex for 1, got %v", err)
	}
}

// TestEmptyStrings tests handling of empty metadata strings.
func TestEmptyStrings(t *testing.T) {
	ir := &ImpulseResponse{
		Metadata: IRMetadata{
			Name:        "",
			Description: "",
			Category:    "",
			Tags:        []string{},
			SampleRate:  48000,
			Channels:    1,
			Length:      10,
		},
		Audio: AudioData{Data: [][]float32{generateTestSamples(10)}},
	}

	buf := newMemFile()
	if err := WriteLibrary(buf, &IRLibrary{Version: CurrentVersion, IRs: []*ImpulseResponse{ir}}); err != nil {
		t.Fatalf("WriteLibrary failed: %v", err)
	}

	buf.Seek(0, io.SeekStart)

	loadedLib, err := ReadLibrary(buf)
	if err != nil {
		t.Fatalf("ReadLibrary failed: %v", err)
	}

	loadedIR := loadedLib.IRs[0]
	if loadedIR.Metadata.Name != "" {
		t.Errorf("expected empty name, got %q", loadedIR.Metadata.Name)
	}

	if loadedIR.Metadata.Description != "" {
		t.Errorf("expected empty description, got %q", loadedIR.Metadata.Description)
	}

	if len(loadedIR.Metadata.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", loadedIR.Metadata.Tags)
	}
}

// TestDuration tests the Duration method.
func TestDuration(t *testing.T) {
	ir := NewImpulseResponse("Test", 48000, 2, [][]float32{
		make([]float32, 96000),
		make([]float32, 96000),
	})

	duration := ir.Duration()
	if math.Abs(duration-2.0) > 0.0001 {
		t.Errorf("expected duration 2.0s, got %v", duration)
	}

	// Test zero sample rate
	ir.Metadata.SampleRate = 0
	if ir.Duration() != 0 {
		t.Errorf("expected 0 duration for zero sample rate")
	}
}

// TestIndexEntryDuration tests the IndexEntry Duration method.
func TestIndexEntryDuration(t *testing.T) {
	entry := IndexEntry{
		SampleRate: 44100,
		Length:     88200,
	}

	duration := entry.Duration()
	if math.Abs(duration-2.0) > 0.0001 {
		t.Errorf("expected duration 2.0s, got %v", duration)
	}
}

// generateTestSamples generates test audio samples (sine wave + noise).
func generateTestSamples(n int) []float32 {
	samples := make([]float32, n)
	for i := range n {
		// Simple exponential decay (typical IR shape)
		t := float64(i) / float64(n)
		samples[i] = float32(math.Exp(-5*t) * math.Sin(2*math.Pi*1000*t/48000))
	}

	return samples
}

// verifyAudioData verifies that audio data matches within f16 precision tolerance.
func verifyAudioData(t *testing.T, original, loaded [][]float32) {
	t.Helper()

	if len(original) != len(loaded) {
		t.Errorf("channel count mismatch: got %d, want %d", len(loaded), len(original))
		return
	}

	for ch := range len(original) {
		if len(original[ch]) != len(loaded[ch]) {
			t.Errorf("channel %d length mismatch: got %d, want %d", ch, len(loaded[ch]), len(original[ch]))
			continue
		}

		for i := range len(original[ch]) {
			orig := original[ch][i]
			load := loaded[ch][i]

			// F16 has ~0.1% relative error for normal values
			// Use absolute error for values near zero
			absErr := math.Abs(float64(orig - load))

			relErr := float64(0)
			if math.Abs(float64(orig)) > 1e-6 {
				relErr = absErr / math.Abs(float64(orig))
			}

			// Allow 1% relative error or 1e-4 absolute error
			if relErr > 0.01 && absErr > 1e-4 {
				t.Errorf("channel %d sample %d: got %v, want %v (relErr=%v, absErr=%v)",
					ch, i, load, orig, relErr, absErr)
			}
		}
	}
}
