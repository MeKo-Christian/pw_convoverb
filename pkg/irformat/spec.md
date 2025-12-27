# IR Library Format Specification (IRLB v1)

## Overview

The IR Library format (`.irlib`) is a chunk-based binary container for storing multiple impulse response (IR) files with metadata. It uses IEEE 754 half-precision (f16) encoding for audio data, providing ~50% storage savings compared to float32.

## Design Goals

- **Compact storage**: F16 encoding reduces file size while maintaining adequate precision for IR data
- **Fast access**: Index chunk enables quick metadata browsing without loading audio data
- **Self-describing**: All metadata embedded in file, no external dependencies
- **Extensible**: Chunk-based design allows future additions without breaking compatibility
- **Simple**: Minimal complexity for reliable parsing

## Byte Order

All multi-byte values are stored in **little-endian** byte order.

## File Structure

```
┌────────────────────────────────────┐
│         FILE HEADER                │
│  Magic: "IRLB" (4 bytes)           │
│  Version: uint16                   │
│  IR Count: uint32                  │
│  Index Offset: uint64              │
├────────────────────────────────────┤
│         IR CHUNK #0                │
│  Chunk ID: "IR--" (4 bytes)        │
│  Chunk Size: uint64                │
│  Metadata Sub-chunk                │
│  Audio Sub-chunk (f16)             │
├────────────────────────────────────┤
│         IR CHUNK #1                │
│           ...                      │
├────────────────────────────────────┤
│         IR CHUNK #N                │
│           ...                      │
├────────────────────────────────────┤
│         INDEX CHUNK                │
│  Chunk ID: "INDX" (4 bytes)        │
│  Chunk Size: uint64                │
│  Index entries (one per IR)        │
└────────────────────────────────────┘
```

## Chunk Types

### File Header (18 bytes)

| Offset | Size | Type   | Description                           |
| ------ | ---- | ------ | ------------------------------------- |
| 0      | 4    | char[] | Magic number: "IRLB"                  |
| 4      | 2    | uint16 | Format version (currently 1)          |
| 6      | 4    | uint32 | Number of IR chunks in file           |
| 10     | 8    | uint64 | Byte offset to INDEX chunk from start |

### IR Chunk

Each IR chunk contains metadata and audio data for one impulse response.

#### IR Chunk Header (12 bytes)

| Offset | Size | Type   | Description                         |
| ------ | ---- | ------ | ----------------------------------- |
| 0      | 4    | char[] | Chunk ID: "IR--"                    |
| 4      | 8    | uint64 | Total chunk size (excluding header) |

#### Metadata Sub-chunk

| Offset   | Size   | Type    | Description                       |
| -------- | ------ | ------- | --------------------------------- |
| 0        | 4      | char[]  | Sub-chunk ID: "META"              |
| 4        | 4      | uint32  | Sub-chunk size (excluding header) |
| 8        | 8      | float64 | Sample rate (Hz)                  |
| 16       | 4      | uint32  | Number of channels                |
| 20       | 4      | uint32  | Samples per channel               |
| 24       | 2      | uint16  | Name length                       |
| 26       | N      | UTF-8   | Name string                       |
| 26+N     | 2      | uint16  | Description length                |
| 28+N     | M      | UTF-8   | Description string                |
| 28+N+M   | 2      | uint16  | Category length                   |
| 30+N+M   | P      | UTF-8   | Category string                   |
| 30+N+M+P | 2      | uint16  | Tag count                         |
| 32+N+M+P | varies | Tag[]   | Array of tags                     |

Each tag is encoded as:
| Offset | Size | Type | Description |
|--------|------|--------|-----------------|
| 0 | 2 | uint16 | Tag length |
| 2 | N | UTF-8 | Tag string |

#### Audio Sub-chunk

| Offset | Size | Type   | Description                       |
| ------ | ---- | ------ | --------------------------------- |
| 0      | 4    | char[] | Sub-chunk ID: "AUDI"              |
| 4      | 4    | uint32 | Sub-chunk size (excluding header) |
| 8      | N    | f16[]  | Interleaved f16 audio samples     |

Audio data is stored as interleaved f16 samples:

- Mono: `s0, s1, s2, ...`
- Stereo: `L0, R0, L1, R1, L2, R2, ...`
- N channels: `ch0_s0, ch1_s0, ..., chN_s0, ch0_s1, ...`

### Index Chunk

The index chunk provides fast access to IR metadata without parsing all IR chunks.

#### Index Chunk Header (12 bytes)

| Offset | Size | Type   | Description                         |
| ------ | ---- | ------ | ----------------------------------- |
| 0      | 4    | char[] | Chunk ID: "INDX"                    |
| 4      | 8    | uint64 | Total chunk size (excluding header) |

#### Index Entry (per IR)

| Offset | Size | Type    | Description                        |
| ------ | ---- | ------- | ---------------------------------- |
| 0      | 8    | uint64  | Byte offset to IR chunk from start |
| 8      | 8    | float64 | Sample rate (Hz)                   |
| 16     | 4    | uint32  | Number of channels                 |
| 20     | 4    | uint32  | Samples per channel                |
| 24     | 2    | uint16  | Name length                        |
| 26     | N    | UTF-8   | Name string                        |
| 26+N   | 2    | uint16  | Category length                    |
| 28+N   | M    | UTF-8   | Category string                    |

## Version History

### Version 1 (Current)

- Initial format release
- F16 audio encoding
- Basic metadata (name, description, category, tags)
- Index chunk for fast browsing

## Precision Notes

IEEE 754 half-precision (f16) provides:

- 1 sign bit
- 5 exponent bits (bias 15)
- 10 mantissa bits (11 bits effective precision)

This gives approximately 3.3 decimal digits of precision, which is adequate for:

- 16-bit source material (typical for impulse responses)
- Audio processing where quantization noise is masked by reverb tails

For critical applications requiring higher precision, consider keeping original float32 sources.

## Alignment

No specific alignment requirements. Readers should not assume aligned access.

## Error Handling

Readers should:

- Verify magic number matches "IRLB"
- Check version is supported (currently only v1)
- Validate chunk sizes don't exceed file bounds
- Skip unknown chunk types for forward compatibility
- Validate sample rates, channel counts are reasonable

## Example

A library with 2 IRs might look like:

```
Offset 0x0000: "IRLB" 0x0001 0x00000002 0x00001234
              (magic) (v1)   (2 IRs)    (index @ 0x1234)

Offset 0x0012: "IR--" 0x0000000000000800
              (IR chunk, 2048 bytes)

Offset 0x001E: "META" 0x00000040
              (metadata, 64 bytes)
              48000.0 (sample rate)
              2 (channels)
              48000 (samples)
              "Large Hall" (name)
              ...

Offset 0x0066: "AUDI" 0x000007C0
              (audio, 1984 bytes = 992 f16 samples)
              [interleaved f16 data]

Offset 0x0826: "IR--" ...
              (second IR chunk)

Offset 0x1234: "INDX" 0x00000080
              (index chunk)
              [index entries]
```
