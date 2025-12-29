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
	"bytes"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
	"unsafe"

	"pw-convoverb/dsp"
	"pw-convoverb/web"

	_ "embed"
)

//go:embed assets/ir-library.irlib
var embeddedIRLibrary []byte

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
	irFile := flag.String("ir", "", "Path to impulse response file (.irlib or legacy .aif)")
	irLibrary := flag.String("ir-library", "", "Path to IR library file (.irlib)")
	irName := flag.String("ir-name", "", "Name of IR to load from library")
	irIndex := flag.Int("ir-index", 0, "Index of IR to load from library (default: 0)")
	listIRs := flag.Bool("list-irs", false, "List available IRs in the library and exit")
	wetLevel := flag.Float64("wet", 0.3, "Wet (reverb) level (0.0-1.0)")
	dryLevel := flag.Float64("dry", 0.7, "Dry (direct) level (0.0-1.0)")
	noTUI := flag.Bool("no-tui", false, "Disable interactive TUI")
	latency := flag.Int("latency", 256, "Processing latency in samples (64, 128, 256, or 512)")
	webPort := flag.Int("port", 8080, "Web server port")
	noBrowser := flag.Bool("no-browser", false, "Don't auto-open browser")
	noWeb := flag.Bool("no-web", false, "Disable web server")
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
		fmt.Println("\nExamples:")
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("  pw-convoverb -ir-library ./ir-library.irlib")
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("  pw-convoverb -ir-library ./ir-library.irlib -ir-name \"Large Hall\"")
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("  pw-convoverb -ir-library ./ir-library.irlib -ir-index 5")
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("  pw-convoverb -ir-library ./ir-library.irlib -list-irs")
		//nolint:forbidigo // CLI help output requires fmt.Println
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// Handle -list-irs: list available IRs and exit
	if *listIRs {
		libraryPath := *irLibrary
		if libraryPath == "" {
			libraryPath = *irFile
		}

		var entries []dsp.IRIndexEntry
		var err error
		var source string

		if libraryPath != "" {
			// List from external file
			entries, err = dsp.ListLibraryIRs(libraryPath)
			source = libraryPath
		} else {
			// List from embedded library
			entries, err = dsp.ListLibraryIRsFromReader(bytes.NewReader(embeddedIRLibrary))
			source = "(embedded)"
		}

		if err != nil {
			//nolint:forbidigo // CLI error output
			fmt.Printf("ERROR: Failed to read IR library: %v\n", err)
			os.Exit(1)
		}

		//nolint:forbidigo // CLI output
		fmt.Printf("Available IRs in %s:\n\n", source)
		for i, entry := range entries {
			channelStr := "mono"
			if entry.Channels == 2 {
				channelStr = "stereo"
			} else if entry.Channels > 2 {
				channelStr = fmt.Sprintf("%dch", entry.Channels)
			}
			//nolint:forbidigo // CLI output
			fmt.Printf("  %3d: %-30s (category: %s, %.0fHz, %s, %.2fs)\n",
				i, entry.Name, entry.Category, entry.SampleRate, channelStr, entry.Duration())
		}
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

	// Configure latency before loading IR
	// Convert samples to block order: 64=6, 128=7, 256=8, 512=9
	var blockOrder int
	switch *latency {
	case 64:
		blockOrder = 6
	case 128:
		blockOrder = 7
	case 256:
		blockOrder = 8
	case 512:
		blockOrder = 9
	default:
		// Find closest valid latency
		if *latency <= 96 {
			blockOrder = 6
		} else if *latency <= 192 {
			blockOrder = 7
		} else if *latency <= 384 {
			blockOrder = 8
		} else {
			blockOrder = 9
		}
		slog.Warn("Invalid latency value, using closest valid", "requested", *latency, "actual", 1<<blockOrder)
	}
	reverb.SetLatency(blockOrder)
	slog.Info("Latency configured", "samples", 1<<blockOrder)

	// Load impulse response
	if *irLibrary != "" {
		// Load from external IR library file
		if err := reverb.LoadImpulseResponseFromLibrary(*irLibrary, *irName, *irIndex); err != nil {
			slog.Error("Failed to load impulse response from library", "library", *irLibrary, "name", *irName, "index", *irIndex, "error", err)
			//nolint:forbidigo // critical error output to user
			fmt.Printf("ERROR: Failed to load impulse response: %v\n", err)
			os.Exit(1)
		}
		if *irName != "" {
			slog.Info("Impulse response loaded from library", "library", *irLibrary, "name", *irName)
		} else {
			slog.Info("Impulse response loaded from library", "library", *irLibrary, "index", *irIndex)
		}
	} else if *irFile != "" {
		// Legacy: load from single file
		if err := reverb.LoadImpulseResponse(*irFile); err != nil {
			slog.Error("Failed to load impulse response", "file", *irFile, "error", err)
			//nolint:forbidigo // critical error output to user
			fmt.Printf("ERROR: Failed to load impulse response: %v\n", err)
			os.Exit(1)
		}
		slog.Info("Impulse response loaded", "file", *irFile)
	} else {
		// Load from embedded library (default)
		if err := reverb.LoadImpulseResponseFromBytes(embeddedIRLibrary, *irName, *irIndex); err != nil {
			slog.Error("Failed to load impulse response from embedded library", "name", *irName, "index", *irIndex, "error", err)
			//nolint:forbidigo // critical error output to user
			fmt.Printf("ERROR: Failed to load impulse response: %v\n", err)
			os.Exit(1)
		}
		if *irName != "" {
			slog.Info("Impulse response loaded from embedded library", "name", *irName)
		} else {
			slog.Info("Impulse response loaded from embedded library", "index", *irIndex)
		}
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

	// Prepare IR list for TUI (always from embedded library for now)
	irList, _ := dsp.ListLibraryIRsFromReader(bytes.NewReader(embeddedIRLibrary))

	// Get initial IR name
	initialIRName := ""
	if *irIndex >= 0 && *irIndex < len(irList) {
		initialIRName = irList[*irIndex].Name
	}

	// Start web server if not disabled
	var webServer *web.Server
	if !*noWeb {
		// Convert IR list to web.IREntry format
		webIRList := make([]web.IREntry, len(irList))
		for i, entry := range irList {
			webIRList[i] = web.IREntry{
				Index:      i,
				Name:       entry.Name,
				Category:   entry.Category,
				SampleRate: entry.SampleRate,
				Channels:   entry.Channels,
				Samples:    entry.Length,
				Duration:   entry.Duration(),
			}
		}

		webServer = web.NewServer(reverb, embeddedIRLibrary, nil, *webPort, *irIndex, initialIRName)
		webServer.SetIRList(webIRList)

		// Register as state listener
		reverb.AddStateListener(webServer)

		// Start web server in background
		go func() {
			slog.Info("Starting web server", "port", *webPort)
			if err := webServer.Start(); err != nil {
				slog.Error("Web server error", "error", err)
			}
		}()

		// Auto-open browser
		if !*noBrowser {
			time.Sleep(200 * time.Millisecond) // Give server time to start
			go func() {
				url := fmt.Sprintf("http://localhost:%d", *webPort)
				if err := web.OpenBrowser(url); err != nil {
					slog.Error("Failed to open browser", "error", err)
				}
			}()
		}

		//nolint:forbidigo // startup message
		fmt.Printf("Web UI available at http://localhost:%d\n", *webPort)
	}

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

		// Run TUI in main thread with IR library data
		runTUI(reverb, embeddedIRLibrary, irList, *irIndex)

		// When TUI returns, quit PipeWire loop
		slog.Info("TUI exited, stopping PipeWire loop")
		C.pw_main_loop_quit(loop)

		// Wait for PipeWire loop to finish cleaning up its internal state
		waitGroup.Wait()
	}

	// Shutdown web server gracefully
	if webServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := webServer.Shutdown(ctx); err != nil {
			slog.Error("Web server shutdown error", "error", err)
		}
	}

	// Cleanup
	C.destroy_pipewire_filter(filterData)
	C.pw_main_loop_destroy(loop)
	slog.Info("Shutdown complete")
}
