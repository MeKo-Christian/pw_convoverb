// Package irformat provides reading and writing of IR library files (.irlib).
//
// The IR library format is a chunk-based binary container for storing multiple
// impulse response (IR) files with metadata. It uses IEEE 754 half-precision
// (f16) encoding for audio data, providing ~50% storage savings compared to float32.
//
// See spec.md for the full format specification.
package irformat

import "errors"

// Format constants.
const (
	// MagicNumber identifies an IRLB file.
	MagicNumber = "IRLB"

	// CurrentVersion is the format version implemented by this package.
	CurrentVersion uint16 = 1

	// Chunk type identifiers.
	ChunkTypeIR    = "IR--"
	ChunkTypeIndex = "INDX"
	ChunkTypeMeta  = "META"
	ChunkTypeAudio = "AUDI"
)

// Header sizes in bytes.
const (
	FileHeaderSize     = 18 // Magic(4) + Version(2) + IRCount(4) + IndexOffset(8)
	ChunkHeaderSize    = 12 // ChunkID(4) + ChunkSize(8)
	SubChunkHeaderSize = 8  // ChunkID(4) + ChunkSize(4)
)

// Errors.
var (
	ErrInvalidMagic       = errors.New("irformat: invalid magic number")
	ErrUnsupportedVersion = errors.New("irformat: unsupported format version")
	ErrInvalidChunk       = errors.New("irformat: invalid chunk")
	ErrCorruptedData      = errors.New("irformat: corrupted data")
	ErrIRNotFound         = errors.New("irformat: IR not found")
	ErrInvalidIndex       = errors.New("irformat: invalid IR index")
)

// IRLibrary represents a collection of impulse responses stored in a single file.
type IRLibrary struct {
	Version uint16
	IRs     []*ImpulseResponse
}

// NewIRLibrary creates a new empty IR library.
func NewIRLibrary() *IRLibrary {
	return &IRLibrary{
		Version: CurrentVersion,
		IRs:     make([]*ImpulseResponse, 0),
	}
}

// AddIR adds an impulse response to the library.
func (lib *IRLibrary) AddIR(ir *ImpulseResponse) {
	lib.IRs = append(lib.IRs, ir)
}

// ImpulseResponse represents a single impulse response with metadata and audio data.
type ImpulseResponse struct {
	Metadata IRMetadata
	Audio    AudioData
}

// NewImpulseResponse creates a new impulse response with the given parameters.
func NewImpulseResponse(name string, sampleRate float64, channels int, data [][]float32) *ImpulseResponse {
	length := 0
	if len(data) > 0 {
		length = len(data[0])
	}

	return &ImpulseResponse{
		Metadata: IRMetadata{
			Name:       name,
			SampleRate: sampleRate,
			Channels:   channels,
			Length:     length,
		},
		Audio: AudioData{
			Data: data,
		},
	}
}

// Duration returns the duration of the impulse response in seconds.
func (ir *ImpulseResponse) Duration() float64 {
	if ir.Metadata.SampleRate <= 0 {
		return 0
	}

	return float64(ir.Metadata.Length) / ir.Metadata.SampleRate
}

// IRMetadata contains descriptive information about an impulse response.
type IRMetadata struct {
	Name        string   // Short name for the IR
	Description string   // Longer description
	Category    string   // Category (e.g., "Hall", "Plate", "Room")
	Tags        []string // Additional tags for organization
	SampleRate  float64  // Sample rate in Hz
	Channels    int      // Number of audio channels
	Length      int      // Samples per channel
}

// AudioData contains the decoded audio samples for an impulse response.
type AudioData struct {
	// Data is organized as [channel][sample]
	// For mono: Data[0] contains all samples
	// For stereo: Data[0] is left, Data[1] is right
	Data [][]float32
}

// IndexEntry contains metadata for fast IR lookup without loading audio data.
type IndexEntry struct {
	Offset     uint64  // Byte offset to IR chunk from file start
	SampleRate float64 // Sample rate in Hz
	Channels   int     // Number of audio channels
	Length     int     // Samples per channel
	Name       string  // IR name
	Category   string  // IR category
}

// Duration returns the duration of the indexed IR in seconds.
func (e *IndexEntry) Duration() float64 {
	if e.SampleRate <= 0 {
		return 0
	}

	return float64(e.Length) / e.SampleRate
}
