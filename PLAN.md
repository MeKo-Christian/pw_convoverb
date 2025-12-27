# Implementation Plan: Custom IR Library Format with F16 Storage

## Overview
Implement a custom chunk-based binary format for storing multiple impulse response (IR) files in a single library file using 16-bit floating point (f16) encoding. This will replace the current placeholder IR loading with efficient, compact storage.

## Critical Files
- `dsp/convolution.go` - IR loading and processing (lines 141-165)
- `main.go` - Entry point for IR loading (lines 137-146)
- New: `pkg/f16/` - F16 conversion utilities
- New: `pkg/irformat/` - Custom IR format handling
- New: `cmd/ir-convert/` - Standalone converter tool

## Architecture Decisions

### File Format Design
- **Container**: Single file containing multiple IRs (library approach)
- **Encoding**: IEEE 754 half-precision (f16) for sample data
- **Structure**: Chunk-based format (similar to IFF/RIFF)
- **Metadata**: Sample rate, channel count, name/description, category/tags per IR

### Chunk Structure
```
[HEADER CHUNK]
  - Magic number: "IRLB" (4 bytes)
  - Version: uint16
  - IR count: uint32
  - Index offset: uint64

[IR CHUNKS] (repeated for each IR)
  - Chunk ID: "IR--" (4 bytes)
  - Chunk size: uint64
  - Metadata sub-chunk
  - Audio data sub-chunk (f16 encoded)

[INDEX CHUNK] (at end of file)
  - List of IR metadata + offsets for fast seeking
```

---

## Phase 1: F16 Handler Implementation

### Goal
Create utilities for converting between float32 and f16 formats using the `golang.org/x/exp/float16` library.

### Tasks

#### Task 1.1: Create F16 Package Structure
- Create `pkg/f16/` directory
- Create `pkg/f16/convert.go`
- Add `golang.org/x/exp/float16` dependency to `go.mod`

#### Task 1.2: Implement F16 Conversion Functions
Create conversion utilities:
- `Float32ToF16([]float32) []byte` - Convert float32 slice to f16 bytes
- `F16ToFloat32([]byte) []float32` - Convert f16 bytes to float32 slice
- `Float32ToF16Interleaved([][]float32) []byte` - Multi-channel interleaved encoding
- `F16ToFloat32Deinterleaved([]byte, channels int) [][]float32` - Multi-channel decoding

#### Task 1.3: Add F16 Unit Tests
Test coverage:
- Round-trip conversion accuracy (float32 → f16 → float32)
- Edge cases: zeros, denormals, infinity, NaN
- Multi-channel interleaving correctness
- Performance benchmarks

#### Task 1.4: Document Precision Trade-offs
- Document expected precision loss (11-bit mantissa vs 24-bit)
- Validate acceptable quality for IR data (typically ~16-bit source material)
- Create comparison utility to measure SNR/THD impact

---

## Phase 2: Custom Binary Format Design

### Goal
Design and implement the chunk-based IR library format with comprehensive metadata support.

### Tasks

#### Task 2.1: Define Format Specification
Create `pkg/irformat/spec.md` documenting:
- File magic number and versioning
- Chunk types and structures
- Metadata schema (sample rate, channels, name, description, category, tags)
- Endianness (suggest little-endian for modern CPUs)
- Alignment requirements

#### Task 2.2: Implement Core Data Structures
Create `pkg/irformat/types.go`:
```go
type IRLibrary struct {
    Version uint16
    IRs     []*ImpulseResponse
}

type ImpulseResponse struct {
    Metadata IRMetadata
    Audio    AudioData
}

type IRMetadata struct {
    Name        string
    Description string
    Category    string
    Tags        []string
    SampleRate  float64
    Channels    int
    Length      int  // samples per channel
}

type AudioData struct {
    Data [][]float32  // Decoded data (channel x samples)
}
```

#### Task 2.3: Implement Writer (`pkg/irformat/writer.go`)
Functions:
- `NewWriter(io.Writer) *Writer` - Create writer
- `Writer.WriteHeader(version uint16, irCount int)` - Write file header
- `Writer.WriteIR(ir *ImpulseResponse)` - Write single IR with metadata + f16 audio
- `Writer.WriteIndex(irs []*ImpulseResponse)` - Write index chunk at EOF
- `Writer.Close()` - Finalize file

Chunk writing:
- Write chunk headers (ID, size)
- Write metadata as structured data (consider encoding/gob or JSON for flexibility)
- Write f16-encoded audio data using Phase 1 utilities
- Track offsets for index generation

#### Task 2.4: Implement Reader (`pkg/irformat/reader.go`)
Functions:
- `NewReader(io.ReadSeeker) (*Reader, error)` - Create reader, parse header
- `Reader.ListIRs() []IRMetadata` - Quick metadata access via index
- `Reader.LoadIR(index int) (*ImpulseResponse, error)` - Load specific IR
- `Reader.LoadIRByName(name string) (*ImpulseResponse, error)` - Search by name
- `Reader.Close()` - Cleanup

Features:
- Parse index chunk first for fast metadata access
- Lazy loading (only decode f16 when requested)
- Validate chunk integrity (size checks, magic numbers)

#### Task 2.5: Add Format Unit Tests
Test scenarios:
- Write and read single IR
- Write and read multi-IR library
- Metadata round-trip accuracy
- F16 audio data integrity
- Index-based seeking
- Error handling (corrupted chunks, invalid metadata)

#### Task 2.6: Add Format Integration Tests
- Create test IR library with varied content:
  - Mono and stereo IRs
  - Different sample rates
  - Various lengths (short/long)
  - Different categories/tags
- Verify complete workflow: write library → read index → load specific IR

---

## Phase 3: AIFF to Custom Format Converter

### Goal
Create a standalone CLI tool to convert .aif files to the custom IR library format.

### Tasks

#### Task 3.1: Create Converter Package Structure
- Create `cmd/ir-convert/` directory
- Create `cmd/ir-convert/main.go`
- Create `internal/aiff/` for AIFF parsing

#### Task 3.2: Implement AIFF Parser (`internal/aiff/parser.go`)
Implement AIFF/AIFC parsing:
- Parse FORM chunk header
- Parse COMM chunk (sample rate, channels, bit depth, compression)
- Parse SSND chunk (audio data offset and block alignment)
- Extract PCM data (support 16-bit and 24-bit)
- Convert PCM integers to float32 normalized [-1.0, 1.0]

Handle AIFF-C compressed formats if needed, or document limitations.

#### Task 3.3: Add AIFF Parser Tests
Test with real files from `assets/`:
- Parse "Large Hall.aif", "Small Church.aif", etc.
- Verify extracted sample rate, channel count, length
- Validate PCM data conversion accuracy
- Test with both 16-bit and 24-bit AIFF files (if present)

#### Task 3.4: Implement Converter CLI (`cmd/ir-convert/main.go`)
Command-line interface:
```
ir-convert [options] <input-directory> <output-file>

Options:
  -recursive       Scan input directory recursively
  -category <name> Set category for all IRs (or infer from directory structure)
  -sample-rate     Optionally resample to target rate
  -normalize       Normalize peak amplitude to -1.0dB or similar
  -verbose         Show progress and details
```

Features:
- Scan directory for .aif files
- Parse each AIFF file
- Extract metadata (use filename as name, directory as category)
- Convert to f16 and write to IR library
- Progress reporting
- Error handling (skip invalid files, report failures)

#### Task 3.5: Implement Metadata Inference
Auto-populate metadata from filesystem:
- Name: from filename (strip extension, clean up)
- Category: from parent directory name or --category flag
- Tags: optionally parse from filename patterns (e.g., "Large_Hall_Reverb" → ["large", "hall"])
- Description: optional, could be read from sidecar .txt files

#### Task 3.6: Add Converter Integration Tests
End-to-end tests:
- Convert `assets/*.aif` to test library
- Verify all 55 files are included
- Validate metadata correctness
- Load resulting library and spot-check audio data
- Performance benchmark (conversion speed)

#### Task 3.7: Create Conversion Script
Create `scripts/convert-assets.sh`:
```bash
#!/bin/bash
# Convert all assets/*.aif to ir-library.irlib
./ir-convert -category "Default" -normalize ./assets ./ir-library.irlib
```

---

## Phase 4: Integration with Convolution Engine

### Goal
Update the convolution reverb to load IRs from the custom format and support library selection.

### Tasks

#### Task 4.1: Update `LoadImpulseResponse` Method
Modify `dsp/convolution.go` (lines 141-165):
- Remove placeholder exponential decay generation
- Accept either:
  - Path to `.irlib` file + IR name/index
  - Path to `.aif` file (for backward compatibility, optional)
- Use `irformat.Reader` to load IR
- Convert loaded IR to `[][]float32` for `r.ir`
- Update overlap-add engines with new IR data

#### Task 4.2: Add IR Selection to CLI
Update `main.go` flags:
```go
var (
    irLibrary = flag.String("ir-library", "", "Path to IR library (.irlib file)")
    irName    = flag.String("ir-name", "", "Name of IR to load from library")
    irIndex   = flag.Int("ir-index", -1, "Index of IR to load from library")

    // Deprecated but supported for backward compat:
    irFile    = flag.String("ir-file", "", "Path to single IR file (.aif)")
)
```

#### Task 4.3: Implement IR Library Listing
Add `-list-irs` flag to show available IRs in a library:
```
pw-convoverb -ir-library ./ir-library.irlib -list-irs

Available IRs in library:
  0: Large Hall (category: Hall, 48kHz, stereo, 3.2s)
  1: Small Church (category: Church, 48kHz, stereo, 2.1s)
  ...
```

#### Task 4.4: Add Runtime IR Switching (Optional Enhancement)
If feasible within PipeWire constraints:
- Add TUI commands to switch IR during runtime
- Preload multiple IRs from library
- Atomic swap of `r.ir` and reinitialize engines
- Consider thread-safety with existing `mu sync.RWMutex`

#### Task 4.5: Update Integration Tests
Test scenarios:
- Load IR from library by name
- Load IR from library by index
- Verify convolution output matches expected behavior
- Test with various IR characteristics (short/long, mono/stereo)

#### Task 4.6: Update Documentation
Update README.md:
- Document new IR library format
- Provide conversion workflow
- Update usage examples
- Add troubleshooting section

---

## Phase 5: Optimization and Polish

### Goal
Optimize performance, add quality-of-life features, and ensure production readiness.

### Tasks

#### Task 5.1: Profile Memory Usage
- Measure memory overhead of f16 vs float32 storage
- Validate 50% reduction in IR data size
- Profile file I/O performance (mmap consideration for large libraries?)
- Optimize chunk reading (buffered I/O)

#### Task 5.2: Add Compression (Optional)
Consider adding optional zlib/zstd compression:
- Compress metadata and audio chunks
- Document size/speed trade-offs
- Make it optional (flag in chunk header)

#### Task 5.3: Add File Validation Tool
Create `ir-validate` subcommand in converter:
```
ir-convert validate <library-file>
```
- Verify chunk integrity
- Check for truncated data
- Validate all IRs can be decoded
- Report statistics (total size, IR count, size savings vs float32)

#### Task 5.4: Error Handling Audit
- Ensure all file I/O operations handle errors gracefully
- Add descriptive error messages
- Consider adding error recovery (e.g., skip corrupted chunks)

#### Task 5.5: Performance Benchmarks
Benchmark key operations:
- AIFF parsing speed
- F16 encoding/decoding speed
- IR library loading speed
- Compare against original placeholder (synthetic IR generation)

#### Task 5.6: Create Example IR Library
- Convert `assets/*.aif` to `examples/default-library.irlib`
- Include in repository or provide download link
- Use as default if no IR specified

---

## Testing Strategy

### Unit Tests
- F16 conversion utilities (Phase 1)
- IR format writer/reader (Phase 2)
- AIFF parser (Phase 3)

### Integration Tests
- End-to-end converter workflow (Phase 3)
- Load and process IR from library (Phase 4)

### Manual Testing
- Convert all 55 AIFF files
- Listen test: compare original AIFF vs library IR convolution quality
- Validate no audible artifacts from f16 quantization

### Regression Tests
- Ensure existing convolution engine behavior unchanged
- Verify dry/wet mix, multi-channel processing still works

---

## Success Criteria

1. ✅ F16 conversion utilities with <0.01% error vs float32
2. ✅ Custom IR library format can store multiple IRs with full metadata
3. ✅ Converter successfully processes all 55 .aif files from assets/
4. ✅ Library files are ~50% smaller than uncompressed float32 equivalent
5. ✅ pw-convoverb can load and use IRs from library with selection by name/index
6. ✅ No audible quality degradation from f16 encoding
7. ✅ All tests passing (unit + integration)
8. ✅ Documentation updated and complete

---

## Risks and Mitigations

### Risk: F16 Precision Loss
**Mitigation**:
- Validate with listening tests
- Compare SNR measurements
- F16 has adequate precision for 16-bit source material (most IRs)
- Consider storing gain/normalization metadata separately in float32 if needed

### Risk: AIFF Parsing Complexity
**Mitigation**:
- Start with simple uncompressed AIFF-C
- Document unsupported formats clearly
- Consider using existing library (e.g., `github.com/go-audio/aiff`) if custom parser proves difficult

### Risk: Large Library File Size
**Mitigation**:
- F16 already provides 50% reduction
- Optional compression in Phase 5
- Chunked format allows partial loading

### Risk: Thread Safety in Runtime IR Switching
**Mitigation**:
- Use existing `sync.RWMutex` in ConvolutionReverb
- Atomic swap pattern: prepare new engines → lock → swap → unlock
- If too complex, defer to future version

---

## Dependencies

### New Go Dependencies
- `golang.org/x/exp/float16` - IEEE 754 half-precision float support
- Optional: `github.com/go-audio/aiff` - If custom parser is insufficient
- Optional: `github.com/klauspost/compress` - For zstd compression in Phase 5

### Existing Codebase
- `dsp/convolution.go` - ConvolutionReverb struct and processing
- `main.go` - CLI entry point and flag handling
- `assets/*.aif` - 55 AIFF files for testing and conversion

---

## Timeline Estimate

- Phase 1 (F16): 2-4 hours
- Phase 2 (Format): 6-8 hours
- Phase 3 (Converter): 6-8 hours
- Phase 4 (Integration): 4-6 hours
- Phase 5 (Polish): 3-5 hours

**Total**: ~21-31 hours of focused development

---

## Future Enhancements

- Web-based IR library browser/editor
- Support for multichannel (5.1, 7.1) IRs
- IR synthesis/editing tools (trim, fade, normalize, EQ)
- Cloud-based IR library repository
- Real-time IR morphing/blending
