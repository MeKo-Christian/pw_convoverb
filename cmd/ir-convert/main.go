// Command ir-convert converts AIFF files to the custom IR library format.
//
// Usage:
//
//	ir-convert [options] <input-directory> <output-file>
//
// Options:
//
//	-recursive     Scan input directory recursively
//	-category      Set category for all IRs (default: infer from directory)
//	-normalize     Normalize peak amplitude to -1.0dB
//	-verbose       Show progress and details
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"pw-convoverb/internal/aiff"
	"pw-convoverb/pkg/irformat"
)

var (
	recursive = flag.Bool("recursive", false, "Scan input directory recursively")
	category  = flag.String("category", "", "Set category for all IRs (default: infer from directory)")
	normalize = flag.Bool("normalize", false, "Normalize peak amplitude to -1.0dB")
	verbose   = flag.Bool("verbose", false, "Show progress and details")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <input-directory> <output-file>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Converts AIFF files to the custom IR library format (.irlib).\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s ./assets ./ir-library.irlib\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -category Hall -normalize ./hall-irs ./halls.irlib\n", os.Args[0])
	}
	flag.Parse()

	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(1)
	}

	inputDir := flag.Arg(0)
	outputFile := flag.Arg(1)

	err := run(inputDir, outputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(inputDir, outputFile string) error {
	// Find AIFF files
	files, err := findAIFFFiles(inputDir, *recursive)
	if err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no .aif files found in %s", inputDir)
	}

	if *verbose {
		fmt.Printf("Found %d AIFF files\n", len(files))
	}

	// Create library
	lib := irformat.NewIRLibrary()

	// Process each file
	for i, filePath := range files {
		if *verbose {
			fmt.Printf("[%d/%d] Processing: %s\n", i+1, len(files), filepath.Base(filePath))
		}

		ir, err := convertFile(filePath, inputDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s: %v\n", filePath, err)
			continue
		}

		lib.AddIR(ir)
	}

	if len(lib.IRs) == 0 {
		return errors.New("no files were successfully converted")
	}

	// Write output file
	outFile, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	if err := irformat.WriteLibrary(outFile, lib); err != nil {
		return fmt.Errorf("failed to write library: %w", err)
	}

	// Get file size
	info, err := outFile.Stat()
	if err == nil && *verbose {
		fmt.Printf("\nLibrary written: %s\n", outputFile)
		fmt.Printf("  IRs: %d\n", len(lib.IRs))
		fmt.Printf("  Size: %.2f MB\n", float64(info.Size())/(1024*1024))
	} else {
		fmt.Printf("Created %s with %d IRs\n", outputFile, len(lib.IRs))
	}

	return nil
}

func findAIFFFiles(dir string, recursive bool) ([]string, error) {
	var files []string

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip subdirectories if not recursive
		if d.IsDir() && path != dir && !recursive {
			return fs.SkipDir
		}

		// Check for AIFF files
		if !d.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".aif" || ext == ".aiff" {
				files = append(files, path)
			}
		}

		return nil
	}
	err := filepath.WalkDir(dir, walkFn)
	if err != nil {
		return nil, err
	}

	return files, nil
}

func convertFile(filePath, baseDir string) (*irformat.ImpulseResponse, error) {
	// Open and parse AIFF file
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	aiffFile, err := aiff.Parse(f)
	if err != nil {
		return nil, err
	}

	// Get audio data
	data := aiffFile.Data

	// Normalize if requested
	if *normalize {
		data = normalizeAudio(data)
	}

	// Infer metadata
	name := inferName(filePath)

	cat := inferCategory(filePath, baseDir)
	if *category != "" {
		cat = *category
	}

	tags := inferTags(name)

	ir := &irformat.ImpulseResponse{
		Metadata: irformat.IRMetadata{
			Name:        name,
			Description: "",
			Category:    cat,
			Tags:        tags,
			SampleRate:  aiffFile.SampleRate,
			Channels:    aiffFile.NumChannels,
			Length:      aiffFile.NumSamples,
		},
		Audio: irformat.AudioData{
			Data: data,
		},
	}

	if *verbose {
		fmt.Printf("    %s: %d ch, %.0f Hz, %d samples (%.2fs)\n",
			name, aiffFile.NumChannels, aiffFile.SampleRate,
			aiffFile.NumSamples, aiffFile.Duration())
	}

	return ir, nil
}

// inferName extracts a clean name from the file path.
func inferName(filePath string) string {
	name := filepath.Base(filePath)
	// Remove extension
	ext := filepath.Ext(name)
	name = strings.TrimSuffix(name, ext)
	// Clean up underscores
	name = strings.ReplaceAll(name, "_", " ")

	return name
}

// inferCategory determines the category from the directory structure.
func inferCategory(filePath, baseDir string) string {
	// Get relative path
	rel, err := filepath.Rel(baseDir, filePath)
	if err != nil {
		return "Default"
	}

	// Use parent directory as category
	dir := filepath.Dir(rel)
	if dir == "." || dir == "" {
		return "Default"
	}

	// Use first directory level as category
	parts := strings.Split(dir, string(filepath.Separator))
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}

	return "Default"
}

// inferTags extracts tags from the filename.
func inferTags(name string) []string {
	// Common reverb-related keywords
	keywords := []string{
		"hall", "room", "plate", "spring", "chamber",
		"church", "ambience", "studio", "vocal", "drum",
		"guitar", "large", "small", "medium", "short", "long",
		"bright", "dark", "warm", "wet", "dry",
	}

	nameLower := strings.ToLower(name)
	var tags []string

	for _, kw := range keywords {
		if strings.Contains(nameLower, kw) {
			tags = append(tags, kw)
		}
	}

	return tags
}

// normalizeAudio normalizes audio to peak at -1.0dB.
func normalizeAudio(data [][]float32) [][]float32 {
	// Find peak across all channels
	var peak float32

	for _, ch := range data {
		for _, sample := range ch {
			abs := sample
			if abs < 0 {
				abs = -abs
			}

			if abs > peak {
				peak = abs
			}
		}
	}

	if peak == 0 {
		return data // Avoid division by zero
	}

	// Target peak at -1.0dB = 10^(-1/20) ≈ 0.891
	targetPeak := float32(math.Pow(10, -1.0/20.0))
	gain := targetPeak / peak

	// Apply gain
	result := make([][]float32, len(data))
	for ch := range data {
		result[ch] = make([]float32, len(data[ch]))
		for i, sample := range data[ch] {
			result[ch][i] = sample * gain
		}
	}

	return result
}
