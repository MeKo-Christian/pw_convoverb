//go:generate sh -c "gcc -shared -o libpw_wrapper.so -fPIC csrc/pw_wrapper.c -I/usr/include/pipewire-0.3 -I/usr/include/spa-0.2 -lpipewire-0.3"

package main

/*
#cgo CFLAGS: -I./csrc -I/usr/include/pipewire-0.3 -I/usr/include/spa-0.2
#cgo LDFLAGS: -L${SRCDIR} -Wl,-rpath,${SRCDIR} -lpw_wrapper -lpipewire-0.3

#include <pipewire/pipewire.h>
#include <spa/param/audio/format-utils.h>
#include <spa/param/audio/format.h>
#include <spa/param/format-utils.h>
#include <spa/utils/type.h>
#include <spa/pod/builder.h>
#include <spa/pod/pod.h>
#include <spa/pod/parser.h>
#include <spa/pod/vararg.h>
#include "pw_wrapper.h"
*/
import "C"

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
	"unsafe"

	"pw-convoverb/dsp"
)

// Audio configuration.
var (
	channels   = 2     // Stereo (modify for 5.1, etc.)
	sampleRate = 48000 // Default sample rate, will be updated by PipeWire
)

// Convolution reverb instance.
var reverb *dsp.ConvolutionReverb

// export log_from_c
//
//export log_from_c
func log_from_c(msg *C.char) {
	slog.Info("C-Side", "msg", C.GoString(msg))
}

// processAudioBuffer processes an INTERLEAVED audio buffer through the reverb (Go wrapper for tests).
func processAudioBuffer(audio []float32) {
	if reverb == nil {
		return
	}

	if len(audio)%channels != 0 {
		return
	}

	samplesPerChannel := len(audio) / channels

	for i := range samplesPerChannel {
		for ch := range channels {
			index := i*channels + ch
			audio[index] = reverb.ProcessSample(audio[index], ch)
		}
	}
}

//export process_channel_go
func process_channel_go(in *C.float, out *C.float, samples C.int, rate C.int, channelIndex C.int) {
	if reverb == nil {
		return
	}

	// Update sample rate if changed
	if rate > 0 {
		reverb.SetSampleRate(float64(rate))
	}

	// Convert C arrays to Go slices
	inBuf := unsafe.Slice((*float32)(unsafe.Pointer(in)), int(samples))
	outBuf := unsafe.Slice((*float32)(unsafe.Pointer(out)), int(samples))

	// Process the block for this specific channel
	reverb.ProcessBlock(inBuf, outBuf, int(channelIndex))
}

func main() {
	// Command-line flags for reverb parameters
	irFile := flag.String("ir", "", "Path to impulse response WAV file")
	wetLevel := flag.Float64("wet", 0.3, "Wet (reverb) level (0.0-1.0)")
	dryLevel := flag.Float64("dry", 0.7, "Dry (direct) level (0.0-1.0)")
	noTUI := flag.Bool("no-tui", false, "Disable interactive TUI")
	debug := flag.Bool("debug", false, "Enable verbose PipeWire debug logging")
	logFile := flag.String("log", "pw-convoverb.log", "Log file path")
	showHelp := flag.Bool("help", false, "Show this help message")

	flag.Parse()

	if *showHelp {
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("PipeWire Convolution Reverb (pw-convoverb)")
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("==========================================")
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("\nA real-time convolution reverb for PipeWire.")
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("\nUsage: pw-convoverb [options]")
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// Setup logging
	file, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
	if err != nil {
		//nolint:forbidigo // error output before logging is initialized
		fmt.Printf("Failed to open log file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	logger := slog.New(slog.NewTextHandler(file, nil))
	slog.SetDefault(logger)
	slog.Info("Starting pw-convoverb", "args", os.Args)

	if *debug {
		C.pw_debug = 1
	}

	// Initialize reverb with default settings
	reverb = dsp.NewConvolutionReverb(float64(sampleRate), channels)
	slog.Info("Reverb initialized", "defaultSampleRate", sampleRate, "channels", channels)

	// Load impulse response if provided
	if *irFile != "" {
		if err := reverb.LoadImpulseResponse(*irFile); err != nil {
			slog.Error("Failed to load impulse response", "file", *irFile, "error", err)
			//nolint:forbidigo // critical error output to user
			fmt.Printf("ERROR: Failed to load impulse response: %v\n", err)
			os.Exit(1)
		}
		slog.Info("Impulse response loaded", "file", *irFile)
	}

	// Configure reverb parameters from command-line flags
	reverb.SetWetLevel(*wetLevel)
	reverb.SetDryLevel(*dryLevel)
	slog.Info("Parameters configured")

	// Initialize PipeWire
	C.pw_init(nil, nil)
	slog.Info("PipeWire initialized")

	// Create main loop
	loop := C.pw_main_loop_new(nil)
	if loop == nil {
		slog.Error("Failed to create PipeWire main loop")
		//nolint:forbidigo // critical error output to user
		fmt.Println("ERROR: Failed to create PipeWire main loop")
		return
	}

	// Create a new PipeWire filter with separate ports for each channel
	filterData := C.create_pipewire_filter(loop, C.int(channels))
	if filterData == nil {
		slog.Error("Failed to create PipeWire filter")
		//nolint:forbidigo // critical error output to user
		fmt.Println("ERROR: Failed to create PipeWire filter")
		C.pw_main_loop_destroy(loop)
		return
	}
	slog.Info("PipeWire filter created")

	if *noTUI {
		//nolint:forbidigo // headless mode startup message
		fmt.Println("Starting PipeWire Convolution Reverb (pw-convoverb)...")
		//nolint:forbidigo // headless mode startup message
		fmt.Println("TUI disabled. Running in headless mode.")
		//nolint:forbidigo // headless mode startup message
		fmt.Println("Log file:", *logFile)
		//nolint:forbidigo // headless mode startup message
		fmt.Println("Press Ctrl+C to exit.")

		// Run in main thread
		C.pw_main_loop_run(loop)
	} else {
		var waitGroup sync.WaitGroup
		waitGroup.Add(1)

		// Run PipeWire loop in background
		go func() {
			defer waitGroup.Done()
			slog.Info("Starting PipeWire main loop")
			C.pw_main_loop_run(loop)
			slog.Info("PipeWire main loop exited")
		}()

		// Give PipeWire a moment to start (optional)
		time.Sleep(100 * time.Millisecond)

		// Run TUI in main thread
		runTUI(reverb)

		// When TUI returns, quit PipeWire loop
		slog.Info("TUI exited, stopping PipeWire loop")
		C.pw_main_loop_quit(loop)

		// Wait for PipeWire loop to finish cleaning up its internal state
		waitGroup.Wait()
	}

	// Cleanup
	C.destroy_pipewire_filter(filterData)
	C.pw_main_loop_destroy(loop)
	slog.Info("Shutdown complete")
}
