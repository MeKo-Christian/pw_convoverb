package irformat

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"pw-convoverb/pkg/f16"
)

// Writer writes IR library files.
type Writer struct {
	w          io.WriteSeeker
	irCount    uint32
	irOffsets  []uint64
	irMetas    []IRMetadata
	currentPos uint64
}

// NewWriter creates a new Writer that writes to w.
// The writer must support seeking to allow writing the index at the end.
func NewWriter(w io.WriteSeeker) *Writer {
	return &Writer{
		w:          w,
		irOffsets:  make([]uint64, 0),
		irMetas:    make([]IRMetadata, 0),
		currentPos: 0,
	}
}

// WriteHeader writes the file header. Must be called before writing any IRs.
// The irCount parameter specifies how many IRs will be written.
func (w *Writer) WriteHeader(irCount int) error {
	w.irCount = uint32(irCount)

	// Write magic number
	if _, err := w.w.Write([]byte(MagicNumber)); err != nil {
		return fmt.Errorf("failed to write magic number: %w", err)
	}

	// Write version
	err := binary.Write(w.w, binary.LittleEndian, CurrentVersion)
	if err != nil {
		return fmt.Errorf("failed to write version: %w", err)
	}

	// Write IR count
	err = binary.Write(w.w, binary.LittleEndian, w.irCount)
	if err != nil {
		return fmt.Errorf("failed to write IR count: %w", err)
	}

	// Write placeholder for index offset (will be updated in Close)
	err = binary.Write(w.w, binary.LittleEndian, uint64(0))
	if err != nil {
		return fmt.Errorf("failed to write index offset placeholder: %w", err)
	}

	w.currentPos = FileHeaderSize

	return nil
}

// WriteIR writes a single impulse response to the file.
// Must be called after WriteHeader and before Close.
func (w *Writer) WriteIR(impulseResponse *ImpulseResponse) error {
	// Record the offset for this IR
	w.irOffsets = append(w.irOffsets, w.currentPos)
	w.irMetas = append(w.irMetas, impulseResponse.Metadata)

	// Build metadata sub-chunk
	metaData := w.buildMetadataSubChunk(&impulseResponse.Metadata)

	// Build audio sub-chunk
	audioData := w.buildAudioSubChunk(&impulseResponse.Audio)

	// Calculate total IR chunk size (metadata + audio, excluding chunk header)
	chunkSize := uint64(len(metaData) + len(audioData))

	// Write IR chunk header
	if _, err := w.w.Write([]byte(ChunkTypeIR)); err != nil {
		return fmt.Errorf("failed to write IR chunk header: %w", err)
	}

	err := binary.Write(w.w, binary.LittleEndian, chunkSize)
	if err != nil {
		return fmt.Errorf("failed to write IR chunk size: %w", err)
	}

	// Write metadata sub-chunk
	if _, err := w.w.Write(metaData); err != nil {
		return fmt.Errorf("failed to write metadata sub-chunk: %w", err)
	}

	// Write audio sub-chunk
	if _, err := w.w.Write(audioData); err != nil {
		return fmt.Errorf("failed to write audio sub-chunk: %w", err)
	}

	w.currentPos += ChunkHeaderSize + chunkSize

	return nil
}

// Close finalizes the file by writing the index chunk and updating the header.
func (w *Writer) Close() error {
	// Record index offset
	indexOffset := w.currentPos

	// Build and write index chunk
	indexData := w.buildIndexChunk()

	// Write index chunk header
	if _, err := w.w.Write([]byte(ChunkTypeIndex)); err != nil {
		return fmt.Errorf("failed to write index chunk header: %w", err)
	}

	err := binary.Write(w.w, binary.LittleEndian, uint64(len(indexData)))
	if err != nil {
		return fmt.Errorf("failed to write index chunk size: %w", err)
	}

	// Write index data
	if _, err := w.w.Write(indexData); err != nil {
		return fmt.Errorf("failed to write index data: %w", err)
	}

	// Seek back to header and update index offset
	if _, err := w.w.Seek(10, io.SeekStart); err != nil { // offset of index_offset field
		return fmt.Errorf("failed to seek to index offset field: %w", err)
	}

	err = binary.Write(w.w, binary.LittleEndian, indexOffset)
	if err != nil {
		return fmt.Errorf("failed to write index offset: %w", err)
	}

	return nil
}

// buildMetadataSubChunk builds the binary metadata sub-chunk.
func (w *Writer) buildMetadataSubChunk(meta *IRMetadata) []byte {
	// Calculate size needed
	size := 8 + 4 + 4 + // sample rate + channels + length
		2 + len(meta.Name) +
		2 + len(meta.Description) +
		2 + len(meta.Category) +
		2 // tag count

	for _, tag := range meta.Tags {
		size += 2 + len(tag)
	}

	buf := make([]byte, SubChunkHeaderSize+size)
	offset := 0

	// Sub-chunk header
	copy(buf[offset:], ChunkTypeMeta)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], uint32(size))
	offset += 4

	// Sample rate (float64)
	binary.LittleEndian.PutUint64(buf[offset:], uint64FromFloat64(meta.SampleRate))
	offset += 8

	// Channels (uint32)
	binary.LittleEndian.PutUint32(buf[offset:], uint32(meta.Channels))
	offset += 4

	// Length (uint32)
	binary.LittleEndian.PutUint32(buf[offset:], uint32(meta.Length))
	offset += 4

	// Name
	binary.LittleEndian.PutUint16(buf[offset:], uint16(len(meta.Name)))
	offset += 2
	copy(buf[offset:], meta.Name)
	offset += len(meta.Name)

	// Description
	binary.LittleEndian.PutUint16(buf[offset:], uint16(len(meta.Description)))
	offset += 2
	copy(buf[offset:], meta.Description)
	offset += len(meta.Description)

	// Category
	binary.LittleEndian.PutUint16(buf[offset:], uint16(len(meta.Category)))
	offset += 2
	copy(buf[offset:], meta.Category)
	offset += len(meta.Category)

	// Tags
	binary.LittleEndian.PutUint16(buf[offset:], uint16(len(meta.Tags)))

	offset += 2
	for _, tag := range meta.Tags {
		binary.LittleEndian.PutUint16(buf[offset:], uint16(len(tag)))
		offset += 2
		copy(buf[offset:], tag)
		offset += len(tag)
	}

	return buf
}

// buildAudioSubChunk builds the binary audio sub-chunk with f16-encoded data.
func (w *Writer) buildAudioSubChunk(audio *AudioData) []byte {
	// Convert to interleaved f16
	f16Data := f16.Float32ToF16Interleaved(audio.Data)

	buf := make([]byte, SubChunkHeaderSize+len(f16Data))
	offset := 0

	// Sub-chunk header
	copy(buf[offset:], ChunkTypeAudio)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(f16Data)))
	offset += 4

	// Audio data
	copy(buf[offset:], f16Data)

	return buf
}

// buildIndexChunk builds the binary index chunk data.
func (w *Writer) buildIndexChunk() []byte {
	// Calculate size
	size := 0
	for i := range w.irMetas {
		size += 8 + 8 + 4 + 4 + // offset + sample rate + channels + length
			2 + len(w.irMetas[i].Name) +
			2 + len(w.irMetas[i].Category)
	}

	buf := make([]byte, size)
	offset := 0

	for i, meta := range w.irMetas {
		// IR offset
		binary.LittleEndian.PutUint64(buf[offset:], w.irOffsets[i])
		offset += 8

		// Sample rate
		binary.LittleEndian.PutUint64(buf[offset:], uint64FromFloat64(meta.SampleRate))
		offset += 8

		// Channels
		binary.LittleEndian.PutUint32(buf[offset:], uint32(meta.Channels))
		offset += 4

		// Length
		binary.LittleEndian.PutUint32(buf[offset:], uint32(meta.Length))
		offset += 4

		// Name
		binary.LittleEndian.PutUint16(buf[offset:], uint16(len(meta.Name)))
		offset += 2
		copy(buf[offset:], meta.Name)
		offset += len(meta.Name)

		// Category
		binary.LittleEndian.PutUint16(buf[offset:], uint16(len(meta.Category)))
		offset += 2
		copy(buf[offset:], meta.Category)
		offset += len(meta.Category)
	}

	return buf
}

// WriteLibrary is a convenience function to write an entire library in one call.
func WriteLibrary(w io.WriteSeeker, lib *IRLibrary) error {
	writer := NewWriter(w)

	err := writer.WriteHeader(len(lib.IRs))
	if err != nil {
		return err
	}

	for _, ir := range lib.IRs {
		err := writer.WriteIR(ir)
		if err != nil {
			return err
		}
	}

	return writer.Close()
}

// uint64FromFloat64 converts a float64 to its bit representation as uint64.
func uint64FromFloat64(f float64) uint64 {
	return math.Float64bits(f)
}
