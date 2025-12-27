package main

import (
	"fmt"
	"math"
	"time"

	"github.com/nsf/termbox-go"
	"pw-convoverb/dsp"
)

const (
	colDef    = termbox.ColorDefault
	colWhite  = termbox.ColorWhite
	colRed    = termbox.ColorRed
	colGreen  = termbox.ColorGreen
	colYellow = termbox.ColorYellow
	colBlue   = termbox.ColorBlue
	colCyan   = termbox.ColorCyan
)

type TUIState struct {
	selectedParam int
	reverb        *dsp.ConvolutionReverb
	exit          bool
}

var paramNames = []string{
	"Wet Level (0-1)",
	"Dry Level (0-1)",
}

func runTUI(reverb *dsp.ConvolutionReverb) {
	err := termbox.Init()
	if err != nil {
		//nolint:forbidigo // TUI initialization error requires direct output
		fmt.Printf("Failed to initialize TUI: %v\n", err)
		return
	}
	defer termbox.Close()

	termbox.SetInputMode(termbox.InputEsc)

	state := &TUIState{
		reverb: reverb,
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
	case 0: // Wet Level
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
	case 1: // Dry Level
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

func draw(state *TUIState) {
	_ = termbox.Clear(colDef, colDef)

	// Header
	printTB(0, 0, colCyan, colDef, "PipeWire Convolution Reverb (pw-convoverb) - Interactive Mode")
	printTB(0, 1, colWhite, colDef, "Sample Rate: 48000 Hz")
	printTB(0, 2, colDef, colDef, "Use Arrows to navigate/adjust. 'q' or Esc to quit.")
	printTB(0, 3, colDef, colDef, "----------------------------------------------------")

	// Parameters
	vals := []string{
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

		printTB(0, 5+i, col, bgColor, fmt.Sprintf("% -20s %s", prefix+name, vals[i]))
	}

	// Metering
	meterY := 10
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
