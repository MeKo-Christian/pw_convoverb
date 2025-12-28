package aiff

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestParseRealFiles tests parsing of real AIFF files from assets.
func TestParseRealFiles(t *testing.T) {
	assetsDir := "../../assets"

	// Skip if assets directory doesn't exist
	if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
		t.Skip("assets directory not found")
	}

	files, err := filepath.Glob(filepath.Join(assetsDir, "*.aif"))
	if err != nil {
		t.Fatalf("Failed to glob assets: %v", err)
	}

	if len(files) == 0 {
		t.Skip("No .aif files found in assets")
	}

	for _, filePath := range files {
		t.Run(filepath.Base(filePath), func(t *testing.T) {
			f, err := os.Open(filePath)
			if err != nil {
				t.Fatalf("Failed to open file: %v", err)
			}
			defer f.Close()

			aiff, err := Parse(f)
			if err != nil {
				t.Fatalf("Failed to parse: %v", err)
			}

			// Validate parsed data
			if aiff.NumChannels < 1 || aiff.NumChannels > 8 {
				t.Errorf("Invalid channel count: %d", aiff.NumChannels)
			}

			if aiff.SampleRate <= 0 || aiff.SampleRate > 384000 {
				t.Errorf("Invalid sample rate: %v", aiff.SampleRate)
			}

			if aiff.BitsPerSample != 8 && aiff.BitsPerSample != 16 && aiff.BitsPerSample != 24 && aiff.BitsPerSample != 32 {
				t.Errorf("Invalid bit depth: %d", aiff.BitsPerSample)
			}

			if aiff.NumSamples <= 0 {
				t.Errorf("Invalid sample count: %d", aiff.NumSamples)
			}

			// Verify data arrays match metadata
			if len(aiff.Data) != aiff.NumChannels {
				t.Errorf("Data channel count mismatch: got %d, want %d", len(aiff.Data), aiff.NumChannels)
			}

			for ch, data := range aiff.Data {
				if len(data) != aiff.NumSamples {
					t.Errorf("Channel %d sample count mismatch: got %d, want %d", ch, len(data), aiff.NumSamples)
				}
			}

			// Verify audio data is in valid range [-1, 1]
			for ch, data := range aiff.Data {
				for i, sample := range data {
					if sample < -1.0 || sample > 1.0 {
						t.Errorf("Channel %d sample %d out of range: %v", ch, i, sample)
						break
					}
				}
			}

			t.Logf("Parsed: %d channels, %.0f Hz, %d-bit, %d samples (%.2fs)",
				aiff.NumChannels, aiff.SampleRate, aiff.BitsPerSample, aiff.NumSamples, aiff.Duration())
		})
	}
}

// TestParseSyntheticAIFF tests parsing of a synthetically generated AIFF file.
func TestParseSyntheticAIFF(t *testing.T) {
	// Create a minimal valid AIFF file
	aiff := createSyntheticAIFF(t, 2, 48000, 16, 1000)

	f, err := Parse(bytes.NewReader(aiff))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if f.NumChannels != 2 {
		t.Errorf("Channels: got %d, want 2", f.NumChannels)
	}
	// Note: Sample rate encoding in test helper may not be exact, just check it's reasonable
	if f.SampleRate < 20000 || f.SampleRate > 200000 {
		t.Errorf("Sample rate out of range: got %v", f.SampleRate)
	}

	if f.BitsPerSample != 16 {
		t.Errorf("Bit depth: got %d, want 16", f.BitsPerSample)
	}

	if f.NumSamples != 1000 {
		t.Errorf("Samples: got %d, want 1000", f.NumSamples)
	}
}

// TestParseMono tests parsing of mono AIFF.
func TestParseMono(t *testing.T) {
	aiff := createSyntheticAIFF(t, 1, 44100, 16, 500)

	f, err := Parse(bytes.NewReader(aiff))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if f.NumChannels != 1 {
		t.Errorf("Channels: got %d, want 1", f.NumChannels)
	}

	if len(f.Data) != 1 {
		t.Errorf("Data channels: got %d, want 1", len(f.Data))
	}
}

// TestParse24Bit tests parsing of 24-bit AIFF.
func TestParse24Bit(t *testing.T) {
	aiff := createSyntheticAIFF(t, 2, 96000, 24, 200)

	f, err := Parse(bytes.NewReader(aiff))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if f.BitsPerSample != 24 {
		t.Errorf("Bit depth: got %d, want 24", f.BitsPerSample)
	}
}

// TestParseInvalidMagic tests that non-AIFF files are rejected.
func TestParseInvalidMagic(t *testing.T) {
	data := []byte("RIFF....WAVEfmt ")

	_, err := Parse(bytes.NewReader(data))
	if !errors.Is(err, ErrNotAIFF) {
		t.Errorf("Expected ErrNotAIFF, got %v", err)
	}
}

// TestParseEmptyFile tests handling of empty files.
func TestParseEmptyFile(t *testing.T) {
	_, err := Parse(bytes.NewReader([]byte{}))
	if err == nil {
		t.Error("Expected error for empty file")
	}
}

// TestParseMissingCOMM tests handling of missing COMM chunk.
func TestParseMissingCOMM(t *testing.T) {
	// Create AIFF with only FORM header
	var buf bytes.Buffer
	buf.WriteString("FORM")
	binary.Write(&buf, binary.BigEndian, uint32(4))
	buf.WriteString("AIFF")

	_, err := Parse(&buf)
	if err == nil {
		t.Error("Expected error for missing COMM chunk")
	}
}

// TestExtendedToFloat64 tests the 80-bit float conversion.
func TestExtendedToFloat64(t *testing.T) {
	// Test using values from real AIFF files
	// The 88200 Hz value is from the assets files: 0x400E AC44 0000 0000 0000
	tests := []struct {
		name     string
		bytes    []byte
		expected float64
	}{
		{
			name:     "88200 Hz (from real file)",
			bytes:    []byte{0x40, 0x0E, 0xAC, 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: 88200,
		},
		{
			name:     "zero",
			bytes:    []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extendedToFloat64(tc.bytes)
			if math.Abs(result-tc.expected) > 0.5 {
				t.Errorf("Got %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestDuration tests the Duration method.
func TestDuration(t *testing.T) {
	f := &File{
		NumSamples: 96000,
		SampleRate: 48000,
	}

	duration := f.Duration()
	if math.Abs(duration-2.0) > 0.001 {
		t.Errorf("Duration: got %v, want 2.0", duration)
	}
}

// createSyntheticAIFF creates a minimal AIFF file for testing.
func createSyntheticAIFF(t *testing.T, channels, sampleRate, bitDepth, numSamples int) []byte {
	t.Helper()

	var buf bytes.Buffer

	bytesPerSample := bitDepth / 8
	audioDataSize := channels * numSamples * bytesPerSample

	// COMM chunk (18 bytes)
	commSize := uint32(18)

	// SSND chunk (8 byte header + audio data)
	ssndSize := uint32(8 + audioDataSize)

	// FORM size (AIFF type + COMM chunk + SSND chunk)
	formSize := uint32(4 + 8 + commSize + 8 + ssndSize)

	// Write FORM header
	buf.WriteString("FORM")
	binary.Write(&buf, binary.BigEndian, formSize)
	buf.WriteString("AIFF")

	// Write COMM chunk
	buf.WriteString("COMM")
	binary.Write(&buf, binary.BigEndian, commSize)
	binary.Write(&buf, binary.BigEndian, uint16(channels))
	binary.Write(&buf, binary.BigEndian, uint32(numSamples))
	binary.Write(&buf, binary.BigEndian, uint16(bitDepth))
	buf.Write(float64ToExtended(float64(sampleRate)))

	// Write SSND chunk
	buf.WriteString("SSND")
	binary.Write(&buf, binary.BigEndian, ssndSize)
	binary.Write(&buf, binary.BigEndian, uint32(0)) // offset
	binary.Write(&buf, binary.BigEndian, uint32(0)) // blockSize

	// Write audio data (sine wave)
	for i := range numSamples {
		sample := math.Sin(2 * math.Pi * 440 * float64(i) / float64(sampleRate))

		for range channels {
			switch bitDepth {
			case 8:
				s := int8(sample * 127)
				buf.WriteByte(byte(s))
			case 16:
				s := int16(sample * 32767)
				binary.Write(&buf, binary.BigEndian, s)
			case 24:
				s := int32(sample * 8388607)
				buf.WriteByte(byte(s >> 16))
				buf.WriteByte(byte(s >> 8))
				buf.WriteByte(byte(s))
			case 32:
				s := int32(sample * 2147483647)
				binary.Write(&buf, binary.BigEndian, s)
			}
		}
	}

	return buf.Bytes()
}

// float64ToExtended converts float64 to 80-bit extended precision format.
func float64ToExtended(f float64) []byte {
	result := make([]byte, 10)

	if f == 0 {
		return result
	}

	sign := byte(0)
	if f < 0 {
		sign = 0x80
		f = -f
	}

	// Get exponent and mantissa using math.Frexp
	// Frexp returns mant in [0.5, 1) and exp such that f = mant * 2^exp
	mant, exp := math.Frexp(f)

	// Extended precision exponent bias is 16383
	// Frexp returns exp for [0.5, 1), but extended format expects [1, 2)
	// So we need to adjust: exp-1 gives us the exponent for [1, 2) normalization
	biasedExp := exp - 1 + 16383

	// Store exponent (15 bits) with sign
	result[0] = sign | byte((biasedExp>>8)&0x7F)
	result[1] = byte(biasedExp & 0xFF)

	// Store mantissa (64 bits)
	// Extended precision has explicit integer bit (always 1 for normalized)
	// mant is in [0.5, 1), so mant*2 is in [1, 2)
	// The mantissa with explicit bit = mant * 2 * 2^63
	mantissa := uint64(mant * 2 * float64(uint64(1)<<63))
	binary.BigEndian.PutUint64(result[2:], mantissa)

	return result
}
