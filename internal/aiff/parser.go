// Package aiff provides parsing of AIFF and AIFF-C audio files.
//
// AIFF (Audio Interchange File Format) is an IFF-based format developed by Apple.
// This parser supports:
//   - Standard AIFF files (uncompressed PCM)
//   - 8-bit, 16-bit, and 24-bit sample depths
//   - Mono and stereo channels
//
// AIFF-C (compressed) files with non-PCM compression are not supported.
package aiff

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// Errors.
var (
	ErrNotAIFF           = errors.New("aiff: not an AIFF file")
	ErrUnsupportedFormat = errors.New("aiff: unsupported format")
	ErrInvalidFile       = errors.New("aiff: invalid file structure")
	ErrMissingChunk      = errors.New("aiff: missing required chunk")
)

// File represents a parsed AIFF file.
type File struct {
	// Audio metadata
	NumChannels   int
	SampleRate    float64
	BitsPerSample int
	NumSamples    int

	// Decoded audio data as float32 in range [-1.0, 1.0]
	// Organized as [channel][sample]
	Data [][]float32
}

// Parse reads and parses an AIFF file from the given reader.
// Returns a File containing the decoded audio data.
func Parse(r io.Reader) (*File, error) {
	// Read FORM chunk header
	var formHeader [12]byte
	if _, err := io.ReadFull(r, formHeader[:]); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}

	// Verify FORM signature
	if string(formHeader[0:4]) != "FORM" {
		return nil, ErrNotAIFF
	}

	// Get form size (big-endian)
	formSize := binary.BigEndian.Uint32(formHeader[4:8])
	_ = formSize // We'll read chunks until EOF

	// Verify AIFF or AIFC type
	formType := string(formHeader[8:12])
	if formType != "AIFF" && formType != "AIFC" {
		return nil, ErrNotAIFF
	}

	file := &File{}
	var commFound, ssndFound bool
	var ssndData []byte

	// Read chunks
	for {
		// Read chunk header
		var chunkHeader [8]byte
		if _, err := io.ReadFull(r, chunkHeader[:]); err != nil {
			if err == io.EOF {
				break
			}

			return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}

		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.BigEndian.Uint32(chunkHeader[4:8])

		// AIFF chunks are padded to even boundaries
		paddedSize := chunkSize
		if paddedSize%2 != 0 {
			paddedSize++
		}

		switch chunkID {
		case "COMM":
			err := file.parseCOMM(r, chunkSize, formType)
			if err != nil {
				return nil, err
			}

			commFound = true

			// Handle padding
			if chunkSize%2 != 0 {
				_, _ = io.ReadFull(r, make([]byte, 1))
			}

		case "SSND":
			var err error

			ssndData, err = file.parseSSND(r, chunkSize)
			if err != nil {
				return nil, err
			}

			ssndFound = true

			// Handle padding
			if chunkSize%2 != 0 {
				_, _ = io.ReadFull(r, make([]byte, 1))
			}

		default:
			// Skip unknown chunks
			if _, err := io.CopyN(io.Discard, r, int64(paddedSize)); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}

				return nil, fmt.Errorf("%w: failed to skip chunk %s: %w", ErrInvalidFile, chunkID, err)
			}
		}
	}

	if !commFound {
		return nil, fmt.Errorf("%w: COMM chunk", ErrMissingChunk)
	}

	if !ssndFound {
		return nil, fmt.Errorf("%w: SSND chunk", ErrMissingChunk)
	}

	// Decode audio data
	err := file.decodeAudio(ssndData)
	if err != nil {
		return nil, err
	}

	return file, nil
}

// parseCOMM parses the COMM (Common) chunk.
func (f *File) parseCOMM(r io.Reader, size uint32, formType string) error {
	// Basic COMM chunk is 18 bytes
	// AIFC adds compression type (4 bytes) and compression name (variable)
	if size < 18 {
		return fmt.Errorf("%w: COMM chunk too small", ErrInvalidFile)
	}

	var comm [18]byte
	if _, err := io.ReadFull(r, comm[:]); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}

	f.NumChannels = int(binary.BigEndian.Uint16(comm[0:2]))
	f.NumSamples = int(binary.BigEndian.Uint32(comm[2:6]))
	f.BitsPerSample = int(binary.BigEndian.Uint16(comm[6:8]))
	f.SampleRate = extendedToFloat64(comm[8:18])

	// Validate
	if f.NumChannels < 1 || f.NumChannels > 8 {
		return fmt.Errorf("%w: unsupported channel count %d", ErrUnsupportedFormat, f.NumChannels)
	}

	if f.BitsPerSample != 8 && f.BitsPerSample != 16 && f.BitsPerSample != 24 && f.BitsPerSample != 32 {
		return fmt.Errorf("%w: unsupported bit depth %d", ErrUnsupportedFormat, f.BitsPerSample)
	}

	if f.SampleRate <= 0 || f.SampleRate > 384000 {
		return fmt.Errorf("%w: invalid sample rate %v", ErrUnsupportedFormat, f.SampleRate)
	}

	// Handle AIFC compression type
	if formType == "AIFC" && size > 18 {
		remaining := size - 18

		comprData := make([]byte, remaining)
		if _, err := io.ReadFull(r, comprData); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}

		if len(comprData) >= 4 {
			comprType := string(comprData[0:4])
			// Only support uncompressed formats
			if comprType != "NONE" && comprType != "none" && comprType != "sowt" {
				return fmt.Errorf("%w: AIFC compression type %q not supported", ErrUnsupportedFormat, comprType)
			}
		}
	} else if size > 18 {
		// Skip extra bytes in COMM chunk
		_, _ = io.CopyN(io.Discard, r, int64(size-18))
	}

	return nil
}

// parseSSND parses the SSND (Sound Data) chunk and returns raw audio bytes.
func (f *File) parseSSND(r io.Reader, size uint32) ([]byte, error) {
	if size < 8 {
		return nil, fmt.Errorf("%w: SSND chunk too small", ErrInvalidFile)
	}

	// Read offset and block size
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}

	offset := binary.BigEndian.Uint32(header[0:4])
	// blockSize := binary.BigEndian.Uint32(header[4:8]) // Usually 0

	// Skip offset bytes if present
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, r, int64(offset)); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}
	}

	// Read audio data
	dataSize := size - 8 - offset

	data := make([]byte, dataSize)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}

	return data, nil
}

// decodeAudio converts raw PCM bytes to float32 audio data.
func (f *File) decodeAudio(data []byte) error {
	bytesPerSample := f.BitsPerSample / 8
	frameSize := bytesPerSample * f.NumChannels
	numFrames := len(data) / frameSize

	// Update NumSamples if it differs from calculated (some files have incorrect COMM values)
	if numFrames < f.NumSamples {
		f.NumSamples = numFrames
	}

	// Allocate output arrays
	f.Data = make([][]float32, f.NumChannels)
	for ch := range f.Data {
		f.Data[ch] = make([]float32, f.NumSamples)
	}

	// Decode frames
	offset := 0

	for frame := range f.NumSamples {
		for ch := range f.NumChannels {
			var sample float32

			switch f.BitsPerSample {
			case 8:
				// 8-bit AIFF is signed
				s := int8(data[offset])
				sample = float32(s) / 128.0
				offset++

			case 16:
				// 16-bit big-endian signed
				s := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
				sample = float32(s) / 32768.0
				offset += 2

			case 24:
				// 24-bit big-endian signed
				b0, b1, b2 := data[offset], data[offset+1], data[offset+2] //nolint:varnamelen // b0-b2 are idiomatic for byte components
				// Sign-extend from 24 to 32 bits
				var s int32
				if b0&0x80 != 0 {
					// Negative: sign-extend with 0xFF in high byte
					s = -1<<24 | int32(b0)<<16 | int32(b1)<<8 | int32(b2)
				} else {
					s = int32(b0)<<16 | int32(b1)<<8 | int32(b2)
				}

				sample = float32(s) / 8388608.0
				offset += 3

			case 32:
				// 32-bit big-endian signed
				s := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
				sample = float32(s) / 2147483648.0
				offset += 4
			}

			f.Data[ch][frame] = sample
		}
	}

	return nil
}

// extendedToFloat64 converts an 80-bit IEEE 754 extended precision float to float64.
// AIFF stores sample rate in this format (10 bytes).
func extendedToFloat64(byteBuffer []byte) float64 {
	if len(byteBuffer) != 10 {
		return 0
	}

	// Extract sign and exponent
	sign := (byteBuffer[0] >> 7) & 1
	exponent := int(binary.BigEndian.Uint16(byteBuffer[0:2])) & 0x7FFF

	// Extract mantissa (64 bits)
	mantissa := binary.BigEndian.Uint64(byteBuffer[2:10])

	// Handle special cases
	if exponent == 0 {
		if mantissa == 0 {
			return 0
		}
		// Denormalized number - not common for sample rates
		return 0
	}

	if exponent == 0x7FFF {
		// Infinity or NaN
		return math.Inf(1)
	}

	// Convert to float64
	// Extended precision has explicit integer bit, float64 has implicit
	// Exponent bias: extended = 16383, double = 1023
	fval := float64(mantissa) / float64(1<<63)
	fval = math.Ldexp(fval, exponent-16383+1)

	if sign == 1 {
		fval = -fval
	}

	return fval
}

// Duration returns the duration of the audio file in seconds.
func (f *File) Duration() float64 {
	if f.SampleRate <= 0 {
		return 0
	}

	return float64(f.NumSamples) / f.SampleRate
}
