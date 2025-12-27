# PipeWire Convolution Reverb (pw-convoverb)

## Project Overview

`pw-convoverb` is a real-time convolution reverb application for Linux using PipeWire. It combines Go for DSP logic and high-level control with C (via cgo) for low-level PipeWire audio stream handling.

### Key Features

- **Hybrid Architecture:** C handles the real-time audio callback and PipeWire API interactions; Go handles the convolution DSP, user interface, and configuration.
- **Convolution Engine:** Implements partitioned low-latency convolution and overlap-add FFT convolution.
- **Custom IR Format:** Uses a custom `.irlib` chunk-based format with f16 encoding for storage efficiency.
- **Interactive TUI:** Terminal-based user interface for real-time control (built with `termbox-go`).
- **Web Interface:** Contains a web server component (`web/`), likely for remote control/visualization.

## Architecture & Directory Structure

- **`cmd/`**: Additional command-line tools (e.g., `ir-convert` for creating `.irlib` files).
- **`csrc/`**: C source code for the PipeWire wrapper (`pw_wrapper.c`, `pw_wrapper.h`). Compiles to `libpw_wrapper.so`.
- **`dsp/`**: Core DSP logic written in Go.
  - `convolution.go`: Main reverb processor and engine management.
  - `lowlatency.go`: Partitioned convolution implementation.
- **`internal/`**: Internal utility packages (e.g., `aiff` parser).
- **`pkg/`**: Reusable Go packages.
  - `irformat/`: Reader/Writer for the `.irlib` format.
  - `f16/`: Float16 conversion utilities.
  - `resampler/`: Audio resampling logic.
- **`web/`**: Web server and frontend assets.
- **`main.go`**: Application entry point.
- **`justfile`**: Task runner configuration.

## Development Workflow

### Prerequisites

- **Go:** v1.24+
- **PipeWire:** `libpipewire-0.3-dev`
- **GCC:** For compiling C bindings.
- **Just:** Task runner.

### Build & Run Commands

The project uses `just` as the primary task runner.

- **Build:** `just build` (Compiles C shared library and Go binary)
- **Run:** `just run` (Builds and executes `./pw-convoverb`)
- **Clean:** `just clean`

### Testing

- **All Tests:** `just test`
- **Unit Tests Only:** `just test-unit`
- **Integration Tests Only:** `just test-integration`
- **Coverage:** `just test-coverage`

### Code Quality

- **Format:** `just fmt` (uses `treefmt`)
- **Lint:** `just lint` (uses `golangci-lint`)
- **Check All:** `just check` (Format, Lint, Test, Tidy)

### IR Library Format (`.irlib`)

The project uses a custom binary format for Impulse Responses:

- **Magic:** `IRLB`
- **Encoding:** IEEE 754 half-precision (f16) audio data.
- **Structure:** Chunk-based (Header, IR Chunks, Index Chunk).
- See `pkg/irformat/spec.md` for the full specification.

## Notes for AI Assistant

- **CGO Usage:** The project relies heavily on CGO. Changes to `csrc/` require rebuilding the shared library (`just build-lib`).
- **Concurrency:** DSP processing happens in a real-time audio callback. Ensure thread safety when interacting with shared state (e.g., using `sync.RWMutex` as seen in `dsp/convolution.go`).
- **Performance:** Code in the critical path (audio callback) must be allocation-free and highly optimized.
