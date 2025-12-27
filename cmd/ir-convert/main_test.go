package main

import (
	"os"
	"path/filepath"
	"testing"

	"pw-convoverb/pkg/irformat"
)

// TestConvertAssetsDirectory tests converting the real assets directory.
func TestConvertAssetsDirectory(t *testing.T) {
	assetsDir := "../../assets"

	// Skip if assets directory doesn't exist
	if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
		t.Skip("assets directory not found")
	}

	// Skip if no .aif files exist (they've been converted to embedded library)
	dirEntries, _ := os.ReadDir(assetsDir)
	hasAIF := false
	for _, entry := range dirEntries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".aif" {
			hasAIF = true
			break
		}
	}
	if !hasAIF {
		t.Skip("no .aif files in assets (already converted to embedded library)")
	}

	// Create temp output file
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "test.irlib")

	// Run conversion
	if err := run(assetsDir, outputFile); err != nil {
		t.Fatalf("Conversion failed: %v", err)
	}

	// Verify output file exists and has reasonable size
	info, err := os.Stat(outputFile)
	if err != nil {
		t.Fatalf("Output file not created: %v", err)
	}
	if info.Size() < 1000 {
		t.Errorf("Output file too small: %d bytes", info.Size())
	}

	// Read library and verify
	f, err := os.Open(outputFile)
	if err != nil {
		t.Fatalf("Failed to open output file: %v", err)
	}
	defer f.Close()

	reader, err := irformat.NewReader(f)
	if err != nil {
		t.Fatalf("Failed to read library: %v", err)
	}

	// Check IR count
	if reader.IRCount() == 0 {
		t.Error("Library has no IRs")
	}
	t.Logf("Library contains %d IRs", reader.IRCount())

	// Verify each IR can be loaded
	entries := reader.ListIRs()
	for i, entry := range entries {
		ir, err := reader.LoadIR(i)
		if err != nil {
			t.Errorf("Failed to load IR %d (%s): %v", i, entry.Name, err)
			continue
		}

		// Validate IR data
		if ir.Metadata.SampleRate <= 0 {
			t.Errorf("IR %d has invalid sample rate: %v", i, ir.Metadata.SampleRate)
		}
		if ir.Metadata.Channels <= 0 {
			t.Errorf("IR %d has invalid channel count: %d", i, ir.Metadata.Channels)
		}
		if len(ir.Audio.Data) != ir.Metadata.Channels {
			t.Errorf("IR %d audio channels mismatch: %d vs %d", i, len(ir.Audio.Data), ir.Metadata.Channels)
		}
		for ch, data := range ir.Audio.Data {
			if len(data) != ir.Metadata.Length {
				t.Errorf("IR %d channel %d length mismatch: %d vs %d", i, ch, len(data), ir.Metadata.Length)
			}
		}
	}
}

// TestInferName tests the name inference function.
func TestInferName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/path/to/Large Hall.aif", "Large Hall"},
		{"/path/to/Small_Church.aif", "Small Church"},
		{"file.aiff", "file"},
		{"/some/dir/My_Great_IR.aif", "My Great IR"},
	}

	for _, tc := range tests {
		result := inferName(tc.input)
		if result != tc.expected {
			t.Errorf("inferName(%q): got %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// TestInferCategory tests the category inference function.
func TestInferCategory(t *testing.T) {
	tests := []struct {
		filePath string
		baseDir  string
		expected string
	}{
		{"/base/file.aif", "/base", "Default"},
		{"/base/Hall/file.aif", "/base", "Hall"},
		{"/base/Plates/Large/file.aif", "/base", "Plates"},
	}

	for _, tc := range tests {
		result := inferCategory(tc.filePath, tc.baseDir)
		if result != tc.expected {
			t.Errorf("inferCategory(%q, %q): got %q, want %q", tc.filePath, tc.baseDir, result, tc.expected)
		}
	}
}

// TestInferTags tests the tag inference function.
func TestInferTags(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
	}{
		{"Large Hall", []string{"hall", "large"}},
		{"Small Bright Room", []string{"room", "small", "bright"}},
		{"Vocal Plate", []string{"plate", "vocal"}},
		{"Unknown IR", nil},
	}

	for _, tc := range tests {
		result := inferTags(tc.name)

		// Check all expected tags are present
		for _, exp := range tc.expected {
			found := false
			for _, tag := range result {
				if tag == exp {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("inferTags(%q): missing expected tag %q", tc.name, exp)
			}
		}
	}
}

// TestNormalizeAudio tests the audio normalization function.
func TestNormalizeAudio(t *testing.T) {
	// Create test data with known peak
	input := [][]float32{
		{0.5, -0.8, 0.3, 0.8},
		{0.2, 0.6, -0.4, 0.1},
	}

	result := normalizeAudio(input)

	// Find peak in result
	var peak float32
	for _, ch := range result {
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

	// Target is -1.0dB ≈ 0.891
	expected := float32(0.891)
	if peak < expected-0.01 || peak > expected+0.01 {
		t.Errorf("Normalized peak: got %v, want ~%v", peak, expected)
	}
}

// TestFileSizeReduction tests that the converted library is smaller than source.
func TestFileSizeReduction(t *testing.T) {
	assetsDir := "../../assets"

	// Skip if assets directory doesn't exist
	if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
		t.Skip("assets directory not found")
	}

	// Skip if no .aif files exist (they've been converted to embedded library)
	dirEntries, _ := os.ReadDir(assetsDir)
	hasAIF := false
	for _, entry := range dirEntries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".aif" {
			hasAIF = true
			break
		}
	}
	if !hasAIF {
		t.Skip("no .aif files in assets (already converted to embedded library)")
	}

	// Calculate total size of source AIFF files
	var sourceSize int64
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		t.Fatalf("Failed to read assets dir: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".aif" {
			info, err := entry.Info()
			if err == nil {
				sourceSize += info.Size()
			}
		}
	}

	// Create library
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "size-test.irlib")

	if err := run(assetsDir, outputFile); err != nil {
		t.Fatalf("Conversion failed: %v", err)
	}

	info, err := os.Stat(outputFile)
	if err != nil {
		t.Fatalf("Failed to stat output: %v", err)
	}

	libSize := info.Size()
	reduction := 1.0 - float64(libSize)/float64(sourceSize)

	t.Logf("Source AIFF files: %.2f MB", float64(sourceSize)/(1024*1024))
	t.Logf("Library file: %.2f MB", float64(libSize)/(1024*1024))
	t.Logf("Size reduction: %.1f%%", reduction*100)

	// We expect at least 25% reduction (AIFF is 24-bit, we use 16-bit f16)
	if reduction < 0.25 {
		t.Errorf("Expected at least 25%% size reduction, got %.1f%%", reduction*100)
	}
}
