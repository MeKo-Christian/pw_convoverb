package irformat

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"pw-convoverb/pkg/f16"
)

// Reader reads IR library files.
type Reader struct {
	r           io.ReadSeeker
	version     uint16
	irCount     uint32
	indexOffset uint64
	index       []IndexEntry
}

// NewReader creates a new Reader and parses the file header.
// Returns an error if the file is not a valid IR library.
func NewReader(r io.ReadSeeker) (*Reader, error) {
	reader := &Reader{r: r}

	err := reader.readHeader()
	if err != nil {
		return nil, err
	}

	err = reader.readIndex()
	if err != nil {
		return nil, err
	}

	return reader, nil
}

// readHeader reads and validates the file header.
func (r *Reader) readHeader() error {
	// Read magic number
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r.r, magic); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	if string(magic) != MagicNumber {
		return ErrInvalidMagic
	}

	// Read version
	err := binary.Read(r.r, binary.LittleEndian, &r.version)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	if r.version != CurrentVersion {
		return fmt.Errorf("%w: got version %d, expected %d", ErrUnsupportedVersion, r.version, CurrentVersion)
	}

	// Read IR count
	err = binary.Read(r.r, binary.LittleEndian, &r.irCount)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	// Read index offset
	err = binary.Read(r.r, binary.LittleEndian, &r.indexOffset)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	return nil
}

// readIndex reads the index chunk for fast metadata access.
func (r *Reader) readIndex() error {
	// Seek to index
	if _, err := r.r.Seek(int64(r.indexOffset), io.SeekStart); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	// Read index chunk header
	chunkID := make([]byte, 4)
	if _, err := io.ReadFull(r.r, chunkID); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	if string(chunkID) != ChunkTypeIndex {
		return fmt.Errorf("%w: expected index chunk, got %q", ErrInvalidChunk, string(chunkID))
	}

	var chunkSize uint64
	err := binary.Read(r.r, binary.LittleEndian, &chunkSize)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	// Read index entries
	r.index = make([]IndexEntry, 0, r.irCount)
	for range uint32(r.irCount) {
		entry, err := r.readIndexEntry()
		if err != nil {
			return err
		}

		r.index = append(r.index, entry)
	}

	return nil
}

// readIndexEntry reads a single index entry.
func (r *Reader) readIndexEntry() (IndexEntry, error) {
	var entry IndexEntry

	// Offset
	if err := binary.Read(r.r, binary.LittleEndian, &entry.Offset); err != nil {
		return entry, fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	// Sample rate
	var sampleRateBits uint64
	if err := binary.Read(r.r, binary.LittleEndian, &sampleRateBits); err != nil {
		return entry, fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	entry.SampleRate = math.Float64frombits(sampleRateBits)

	// Channels
	var channels uint32
	if err := binary.Read(r.r, binary.LittleEndian, &channels); err != nil {
		return entry, fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	entry.Channels = int(channels)

	// Length
	var length uint32
	if err := binary.Read(r.r, binary.LittleEndian, &length); err != nil {
		return entry, fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	entry.Length = int(length)

	// Name
	name, err := r.readString()
	if err != nil {
		return entry, err
	}

	entry.Name = name

	// Category
	category, err := r.readString()
	if err != nil {
		return entry, err
	}

	entry.Category = category

	return entry, nil
}

// readString reads a length-prefixed UTF-8 string.
func (r *Reader) readString() (string, error) {
	var length uint16
	err := binary.Read(r.r, binary.LittleEndian, &length)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	if length == 0 {
		return "", nil
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r.r, data); err != nil {
		return "", fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	return string(data), nil
}

// Version returns the format version of the library.
func (r *Reader) Version() uint16 {
	return r.version
}

// IRCount returns the number of IRs in the library.
func (r *Reader) IRCount() int {
	return int(r.irCount)
}

// ListIRs returns the metadata for all IRs in the library.
// This uses the index and does not load audio data.
func (r *Reader) ListIRs() []IndexEntry {
	result := make([]IndexEntry, len(r.index))
	copy(result, r.index)

	return result
}

// LoadIR loads a specific IR by index.
func (r *Reader) LoadIR(index int) (*ImpulseResponse, error) {
	if index < 0 || index >= len(r.index) {
		return nil, ErrInvalidIndex
	}

	entry := r.index[index]

	// Seek to IR chunk
	if _, err := r.r.Seek(int64(entry.Offset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	return r.readIRChunk()
}

// LoadIRByName loads an IR by name.
// Returns ErrIRNotFound if no IR with the given name exists.
func (r *Reader) LoadIRByName(name string) (*ImpulseResponse, error) {
	for i, entry := range r.index {
		if entry.Name == name {
			return r.LoadIR(i)
		}
	}

	return nil, ErrIRNotFound
}

// readIRChunk reads a complete IR chunk including metadata and audio.
func (r *Reader) readIRChunk() (*ImpulseResponse, error) {
	// Read IR chunk header
	chunkID := make([]byte, 4)
	if _, err := io.ReadFull(r.r, chunkID); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	if string(chunkID) != ChunkTypeIR {
		return nil, fmt.Errorf("%w: expected IR chunk, got %q", ErrInvalidChunk, string(chunkID))
	}

	var chunkSize uint64
	err := binary.Read(r.r, binary.LittleEndian, &chunkSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	ir := &ImpulseResponse{}

	// Read metadata sub-chunk
	err = r.readMetadataSubChunk(&ir.Metadata)
	if err != nil {
		return nil, err
	}

	// Read audio sub-chunk
	err = r.readAudioSubChunk(&ir.Audio, ir.Metadata.Channels, ir.Metadata.Length)
	if err != nil {
		return nil, err
	}

	return ir, nil
}

// readMetadataSubChunk reads the metadata sub-chunk.
func (r *Reader) readMetadataSubChunk(meta *IRMetadata) error {
	// Read sub-chunk header
	chunkID := make([]byte, 4)
	if _, err := io.ReadFull(r.r, chunkID); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	if string(chunkID) != ChunkTypeMeta {
		return fmt.Errorf("%w: expected metadata sub-chunk, got %q", ErrInvalidChunk, string(chunkID))
	}

	var subChunkSize uint32
	if err := binary.Read(r.r, binary.LittleEndian, &subChunkSize); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	// Sample rate
	var sampleRateBits uint64
	if err := binary.Read(r.r, binary.LittleEndian, &sampleRateBits); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	meta.SampleRate = math.Float64frombits(sampleRateBits)

	// Channels
	var channels uint32
	if err := binary.Read(r.r, binary.LittleEndian, &channels); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	meta.Channels = int(channels)

	// Length
	var length uint32
	if err := binary.Read(r.r, binary.LittleEndian, &length); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	meta.Length = int(length)

	// Name
	name, err := r.readString()
	if err != nil {
		return err
	}

	meta.Name = name

	// Description
	description, err := r.readString()
	if err != nil {
		return err
	}

	meta.Description = description

	// Category
	category, err := r.readString()
	if err != nil {
		return err
	}

	meta.Category = category

	// Tags
	var tagCount uint16
	if err := binary.Read(r.r, binary.LittleEndian, &tagCount); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	meta.Tags = make([]string, tagCount)
	for i := range tagCount {
		tag, err := r.readString()
		if err != nil {
			return err
		}

		meta.Tags[i] = tag
	}

	return nil
}

// readAudioSubChunk reads the audio sub-chunk and decodes f16 data.
func (r *Reader) readAudioSubChunk(audio *AudioData, channels, length int) error {
	// Read sub-chunk header
	chunkID := make([]byte, 4)
	if _, err := io.ReadFull(r.r, chunkID); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	if string(chunkID) != ChunkTypeAudio {
		return fmt.Errorf("%w: expected audio sub-chunk, got %q", ErrInvalidChunk, string(chunkID))
	}

	var subChunkSize uint32
	err := binary.Read(r.r, binary.LittleEndian, &subChunkSize)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	// Read f16 data
	f16Data := make([]byte, subChunkSize)
	if _, err := io.ReadFull(r.r, f16Data); err != nil {
		return fmt.Errorf("%w: %w", ErrCorruptedData, err)
	}

	// Decode f16 to float32
	audio.Data = f16.F16ToFloat32Deinterleaved(f16Data, channels)

	return nil
}

// Close closes the reader. Currently a no-op but provided for interface consistency.
func (r *Reader) Close() error {
	return nil
}

// ReadLibrary is a convenience function to read an entire library in one call.
func ReadLibrary(r io.ReadSeeker) (*IRLibrary, error) {
	reader, err := NewReader(r)
	if err != nil {
		return nil, err
	}

	lib := &IRLibrary{
		Version: reader.version,
		IRs:     make([]*ImpulseResponse, 0, reader.irCount),
	}

	for i := range reader.irCount {
		ir, err := reader.LoadIR(int(i))
		if err != nil {
			return nil, fmt.Errorf("failed to load IR %d: %w", i, err)
		}

		lib.IRs = append(lib.IRs, ir)
	}

	return lib, nil
}
