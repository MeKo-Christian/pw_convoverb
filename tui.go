package main

import (
	"fmt"
	"math"
	"time"

	"github.com/nsf/termbox-go"
	"pw-convoverb/dsp"
)

const (
	colDef     = termbox.ColorDefault
	colWhite   = termbox.ColorWhite
	colRed     = termbox.ColorRed
	colGreen   = termbox.ColorGreen
	colYellow  = termbox.ColorYellow
	colBlue    = termbox.ColorBlue
	colCyan    = termbox.ColorCyan
	colMagenta = termbox.ColorMagenta
)

type TUIState struct {
	selectedParam int
	reverb        *dsp.ConvolutionReverb
	exit          bool

	// IR library data
	irLibraryData []byte             // Embedded IR library bytes
	irList        []dsp.IRIndexEntry // List of available IRs
	currentIRIdx  int                // Currently loaded IR index
	currentIRName string             // Currently loaded IR name
	irBrowseMode  bool               // True when browsing IR list
	irBrowseIdx   int                // Index in IR browser
}

var paramNames = []string{
	"Impulse Response",
	"Wet Level (0-1)",
	"Dry Level (0-1)",
}

func runTUI(reverb *dsp.ConvolutionReverb, irLibraryData []byte, irList []dsp.IRIndexEntry, initialIRIdx int) {
	err := termbox.Init()
	if err != nil {
		//nolint:forbidigo // TUI initialization error requires direct output
		fmt.Printf("Failed to initialize TUI: %v\n", err)
		return
	}
	defer termbox.Close()

	termbox.SetInputMode(termbox.InputEsc)

	initialName := ""
	if initialIRIdx >= 0 && initialIRIdx < len(irList) {
		initialName = irList[initialIRIdx].Name
	}

	state := &TUIState{
		reverb:        reverb,
		irLibraryData: irLibraryData,
		irList:        irList,
		currentIRIdx:  initialIRIdx,
		currentIRName: initialName,
		irBrowseIdx:   initialIRIdx,
	}

	eventQueue := make(chan termbox.Event)

	go func() {
		for {
			eventQueue <- termbox.PollEvent()
		}
	}()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	draw(state)

	for !state.exit {
		select {
		case ev := <-eventQueue:
			switch ev.Type {
			case termbox.EventKey:
				handleKey(ev, state)
			case termbox.EventResize:
				draw(state)
			}
		case <-ticker.C:
			draw(state)
		}
	}
}

func handleKey(ev termbox.Event, s *TUIState) {
	// Handle IR browse mode separately
	if s.irBrowseMode {
		handleIRBrowseKey(ev, s)
		return
	}

	if ev.Key == termbox.KeyEsc || ev.Ch == 'q' {
		s.exit = true
		return
	}

	// Navigation
	switch ev.Key {
	case termbox.KeyArrowUp:
		s.selectedParam--
		if s.selectedParam < 0 {
			s.selectedParam = len(paramNames) - 1
		}
	case termbox.KeyArrowDown:
		s.selectedParam++
		if s.selectedParam >= len(paramNames) {
			s.selectedParam = 0
		}
	}

	// Adjustment
	switch s.selectedParam {
	case 0: // Impulse Response - Enter browse mode on left/right or Enter
		if ev.Key == termbox.KeyArrowRight || ev.Key == termbox.KeyArrowLeft || ev.Key == termbox.KeyEnter {
			s.irBrowseMode = true
			s.irBrowseIdx = s.currentIRIdx
		}
	case 1: // Wet Level
		change := 0.0
		if ev.Key == termbox.KeyArrowRight {
			change = 0.05
		}

		if ev.Key == termbox.KeyArrowLeft {
			change = -0.05
		}

		if change != 0 {
			s.reverb.SetWetLevel(s.reverb.GetWetLevel() + change)
		}
	case 2: // Dry Level
		change := 0.0
		if ev.Key == termbox.KeyArrowRight {
			change = 0.05
		}

		if ev.Key == termbox.KeyArrowLeft {
			change = -0.05
		}

		if change != 0 {
			s.reverb.SetDryLevel(s.reverb.GetDryLevel() + change)
		}
	}
}

func handleIRBrowseKey(ev termbox.Event, s *TUIState) {
	switch ev.Key {
	case termbox.KeyEsc:
		// Cancel browsing, revert to current IR
		s.irBrowseMode = false
		s.irBrowseIdx = s.currentIRIdx
	case termbox.KeyEnter:
		// Load the selected IR
		if s.irBrowseIdx != s.currentIRIdx && len(s.irLibraryData) > 0 {
			name, err := s.reverb.SwitchIR(s.irLibraryData, s.irBrowseIdx)
			if err == nil {
				s.currentIRIdx = s.irBrowseIdx
				s.currentIRName = name
			}
		}
		s.irBrowseMode = false
	case termbox.KeyArrowUp:
		s.irBrowseIdx--
		if s.irBrowseIdx < 0 {
			s.irBrowseIdx = len(s.irList) - 1
		}
	case termbox.KeyArrowDown:
		s.irBrowseIdx++
		if s.irBrowseIdx >= len(s.irList) {
			s.irBrowseIdx = 0
		}
	case termbox.KeyPgup:
		s.irBrowseIdx -= 10
		if s.irBrowseIdx < 0 {
			s.irBrowseIdx = 0
		}
	case termbox.KeyPgdn:
		s.irBrowseIdx += 10
		if s.irBrowseIdx >= len(s.irList) {
			s.irBrowseIdx = len(s.irList) - 1
		}
	}
}

func draw(state *TUIState) {
	_ = termbox.Clear(colDef, colDef)

	// Check if we're in IR browse mode
	if state.irBrowseMode {
		drawIRBrowser(state)
		return
	}

	// Header
	printTB(0, 0, colCyan, colDef, "PipeWire Convolution Reverb (pw-convoverb) - Interactive Mode")
	printTB(0, 1, colWhite, colDef, "Sample Rate: 48000 Hz")
	printTB(0, 2, colDef, colDef, "Use Arrows to navigate/adjust. 'q' or Esc to quit.")
	printTB(0, 3, colDef, colDef, "----------------------------------------------------")

	// Parameters
	irDisplayName := state.currentIRName
	if irDisplayName == "" {
		irDisplayName = "(none)"
	}
	if len(irDisplayName) > 30 {
		irDisplayName = irDisplayName[:27] + "..."
	}

	vals := []string{
		irDisplayName,
		fmt.Sprintf("%.2f", state.reverb.GetWetLevel()),
		fmt.Sprintf("%.2f", state.reverb.GetDryLevel()),
	}

	for i, name := range paramNames {
		col := colWhite
		bgColor := colDef
		prefix := "  "

		if i == state.selectedParam {
			col = colDef       // Black usually if bg is white
			bgColor = colWhite // Highlight
			prefix = "> "
		}

		line := fmt.Sprintf("%-22s %s", prefix+name, vals[i])
		printTB(0, 5+i, col, bgColor, line)

		// Add hint for IR parameter
		if i == 0 && i == state.selectedParam {
			printTB(len(line)+2, 5+i, colYellow, colDef, "[Enter to browse]")
		}
	}

	// Metering
	meterY := 11
	printTB(0, meterY, colYellow, colDef, "Meters:")

	// Convert linear to dB for display
	linToDB := func(l float32) float64 {
		if l <= 1e-9 {
			return -96.0
		}
		return 20 * math.Log10(float64(l))
	}

	// Get metrics from reverb
	inL, outL, revL := state.reverb.GetMetrics(0)
	inR, outR, revR := state.reverb.GetMetrics(1)

	inLdB := linToDB(inL)
	inRdB := linToDB(inR)
	outLdB := linToDB(outL)
	outRdB := linToDB(outR)
	revLdB := linToDB(revL)
	revRdB := linToDB(revR)

	drawMeter(meterY+2, "In L ", inLdB, colGreen)
	drawMeter(meterY+3, "In R ", inRdB, colGreen)

	drawMeter(meterY+5, "Rev L", revLdB, colRed)
	drawMeter(meterY+6, "Rev R", revRdB, colRed)

	drawMeter(meterY+8, "Out L", outLdB, colBlue)
	drawMeter(meterY+9, "Out R", outRdB, colBlue)

	termbox.Flush()
}

func drawIRBrowser(state *TUIState) {
	w, h := termbox.Size()

	// Header
	printTB(0, 0, colMagenta, colDef, "Select Impulse Response")
	printTB(0, 1, colDef, colDef, "Use Up/Down to browse, PgUp/PgDn for fast scroll")
	printTB(0, 2, colDef, colDef, "Enter to select, Esc to cancel")
	printTB(0, 3, colDef, colDef, "─────────────────────────────────────────────────────────────────")

	// Calculate visible range
	listStartY := 5
	listHeight := h - listStartY - 2
	if listHeight < 5 {
		listHeight = 5
	}

	// Scroll to keep selected item visible
	scrollOffset := 0
	if state.irBrowseIdx >= listHeight {
		scrollOffset = state.irBrowseIdx - listHeight + 1
	}

	// Draw IR list
	for i := 0; i < listHeight && scrollOffset+i < len(state.irList); i++ {
		idx := scrollOffset + i
		entry := state.irList[idx]

		col := colWhite
		bgColor := colDef
		prefix := "  "

		if idx == state.irBrowseIdx {
			col = colDef
			bgColor = colWhite
			prefix = "> "
		}

		// Mark current IR
		suffix := ""
		if idx == state.currentIRIdx {
			suffix = " [current]"
		}

		// Format: "  3: Large Hall (Hall, 48kHz, stereo, 2.5s)"
		channelStr := "mono"
		if entry.Channels == 2 {
			channelStr = "stereo"
		} else if entry.Channels > 2 {
			channelStr = fmt.Sprintf("%dch", entry.Channels)
		}

		name := entry.Name
		maxNameLen := 25
		if len(name) > maxNameLen {
			name = name[:maxNameLen-3] + "..."
		}

		line := fmt.Sprintf("%s%3d: %-25s (%s, %.0fkHz, %s, %.1fs)%s",
			prefix, idx, name, entry.Category, entry.SampleRate/1000, channelStr, entry.Duration(), suffix)

		// Truncate to screen width
		if len(line) > w-1 {
			line = line[:w-1]
		}

		printTB(0, listStartY+i, col, bgColor, line)
	}

	// Footer with scroll indicator
	if len(state.irList) > listHeight {
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d",
			scrollOffset+1, min(scrollOffset+listHeight, len(state.irList)), len(state.irList))
		printTB(0, h-1, colYellow, colDef, scrollInfo)
	}

	termbox.Flush()
}

func drawMeter(yPos int, label string, db float64, color termbox.Attribute) {
	const (
		barWidth = 60
		xPos     = 2
		minDB    = -96.0
		maxDB    = 6.0
	)

	if db < minDB {
		db = minDB
	}

	if db > maxDB {
		db = maxDB
	}

	ratio := (db - minDB) / (maxDB - minDB)
	filled := int(ratio * float64(barWidth))

	printTB(xPos, yPos, colDef, colDef, fmt.Sprintf("%s [%-6.1f dB] ", label, db))

	// Draw bar
	startX := xPos + 15

	for i := range barWidth {
		var barChar rune
		bgCol := colDef

		if i < filled {
			barChar = '█'
		} else {
			barChar = '░'
		}

		termbox.SetCell(startX+i, yPos, barChar, color, bgCol)
	}
}

func printTB(x, y int, fg, bg termbox.Attribute, msg string) {
	for _, c := range msg {
		termbox.SetCell(x, y, c, fg, bg)
		x++
	}
}
