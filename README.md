# PipeWire Convolution Reverb (pw-convoverb)

[![CI](https://github.com/MeKo-Christian/pw-convoverb/actions/workflows/test.yaml/badge.svg)](https://github.com/MeKo-Christian/pw-convoverb/actions/workflows/test.yaml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/MeKo-Christian/pw-convoverb)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A real-time convolution reverb implemented in Go, using PipeWire for audio I/O.

## Overview

This project implements a PipeWire filter node that performs convolution reverb on audio streams. The DSP processing is written in Go for maintainability, while PipeWire integration is handled through C bindings (cgo).

**Status**: Initial structure implemented. Convolution reverb DSP implementation is a placeholder and needs to be completed with proper FFT-based partitioned convolution for production use.

## Architecture

- **[csrc/pw_wrapper.c](csrc/pw_wrapper.c)** - C wrapper for PipeWire API, handles stream creation and audio callbacks
- **[csrc/pw_wrapper.h](csrc/pw_wrapper.h)** - Header file with function declarations and type definitions
- **[main.go](main.go)** - Go implementation of the reverb DSP algorithm and main event loop
- **[dsp/convolution.go](dsp/convolution.go)** - Convolution reverb implementation
- **libpw_wrapper.so** - Compiled shared library (generated)

### How It Works

1. PipeWire creates an audio stream configured as a filter node with separate ports for each channel (e.g., FL, FR).
2. Audio buffers arrive via the `on_process` callback in C, which processes each channel individually.
3. The callback invokes `process_channel_go` which processes samples through the convolution reverb.
4. The reverb convolves the input signal with an impulse response (IR) to create realistic reverberation.
5. Processed audio is queued back to PipeWire's output.

## Current Status

### Implemented

- [x] PipeWire stream initialization and event registration
- [x] Audio buffer processing loop with real-time callbacks
- [x] Basic convolution reverb structure with:
  - [x] Impulse response loading (placeholder)
  - [x] Wet/dry mix control
  - [x] Per-channel processing
- [x] Stereo audio support via separate planar ports (FL, FR)
- [x] Adaptable sample rate support (automatically negotiated)
- [x] Bidirectional I/O (filter node)
- [x] Command-line parameter configuration
- [x] Build system (justfile)
- [x] Interactive TUI

### TODO

- [ ] Implement WAV file loading for impulse responses
- [ ] Implement FFT-based partitioned convolution (current implementation is placeholder time-domain)
- [ ] Add proper metering for TUI display
- [ ] Implement comprehensive test suite
- [ ] Add benchmarks for performance optimization
- [ ] Support for various IR file formats

## Reverb Parameters

All parameters can be configured via command-line flags (see Usage section):

- **IR File**: Path to impulse response WAV file
- **Wet Level**: Reverb (wet) signal level (0.0-1.0, default: 0.3)
- **Dry Level**: Direct (dry) signal level (0.0-1.0, default: 0.7)
- **Channels**: 2 (Exposed as separate `FL` and `FR` green ports)
- **Sample Rate**: Adaptable (Negotiated by PipeWire, reverb updates automatically)

## Building

Using the justfile (recommended):

```bash
# Build everything (C library + Go binary)
just build

# Run tests
just test

# Clean build artifacts
just clean
```

Manual compilation:

```bash
# Generate the C shared library
go generate

# Build the Go binary
go build -o pw-convoverb
```

## Dependencies

- PipeWire development libraries (`libpipewire-0.3-dev`)
- Go 1.24 or later
- GCC
- [just](https://github.com/casey/just) (optional, for build automation)

### Ubuntu/Debian

```bash
sudo apt-get install libpipewire-0.3-dev
```

## Usage

Run with default settings:

```bash
./pw-convoverb
```

Run with custom parameters and impulse response:

```bash
./pw-convoverb -ir /path/to/impulse.wav -wet 0.5 -dry 0.5
```

Show all available options:

```bash
./pw-convoverb -help
```

### Available Command-Line Options

- `-ir` - Path to impulse response WAV file
- `-wet` - Wet (reverb) level (0.0-1.0, default: 0.3)
- `-dry` - Dry (direct) level (0.0-1.0, default: 0.7)
- `-no-tui` - Disable interactive TUI
- `-debug` - Enable verbose PipeWire debug logging
- `-log` - Log file path (default: pw-convoverb.log)
- `-help` - Show help message

The filter will appear as "Convolution Reverb" in PipeWire's audio graph and can be connected using tools like `pw-link` or `qpwgraph`.

### Interactive Mode

The reverb features a terminal-based UI for real-time parameter adjustment and metering:

- Use arrow keys to navigate and adjust parameters
- Real-time input/output level meters (green/blue bars)
- Reverb level meters (red bars) show reverb activity
- Press `q` or `Esc` to quit

## Testing

The project structure includes placeholders for tests. Implementation pending.

### Test Organization

- **Unit Tests** - Test the core DSP convolution algorithm in isolation
- **Integration Tests** - Test the full signal path from C boundary through reverb

### Running Tests

```bash
# Run all tests
just test

# Run unit tests only
just test-unit

# Run integration tests only
just test-integration

# Run tests with coverage
just test-coverage
```

## Performance Considerations

The current implementation uses simple time-domain convolution, which is inefficient for long impulse responses. For production use, the following optimizations are recommended:

1. **FFT-based partitioned convolution** - Split the IR into partitions and use overlap-add FFT convolution
2. **Zero-latency partitioning** - Use uniform or non-uniform partitioning for low latency
3. **SIMD optimization** - Vectorize FFT and mixing operations
4. **Multi-threading** - Process channels in parallel

## License

MIT License. See [LICENSE](LICENSE) for details.
