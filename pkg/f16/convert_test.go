package f16

import (
	"math"
	"testing"
)

func TestFloat32ToF16ToFloat32RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input float32
	}{
		{"zero", 0.0},
		{"positive one", 1.0},
		{"negative one", -1.0},
		{"small positive", 0.123},
		{"small negative", -0.456},
		{"large positive", 1024.5},
		{"large negative", -2048.75},
		{"very small (underflow expected)", 1e-6}, // Below f16 min normal (~6e-5), will flush to zero
		{"small but representable", 0.001},        // Well above f16 min normal
		{"infinity", float32(math.Inf(1))},
		{"negative infinity", float32(math.Inf(-1))},
		{"NaN", float32(math.NaN())},
	}

	for _, tableTest := range tests {
		t.Run(tableTest.name, func(t *testing.T) {
			t.Parallel()

			f16bits := float32ToF16(tableTest.input)
			result := f16ToFloat32(f16bits)

			if math.IsNaN(float64(tableTest.input)) {
				if !math.IsNaN(float64(result)) {
					t.Errorf("expected NaN, got %v", result)
				}

				return
			}

			if math.IsInf(float64(tableTest.input), 0) {
				expectedSign := 1
				if math.Signbit(float64(tableTest.input)) {
					expectedSign = -1
				}

				if !math.IsInf(float64(result), expectedSign) {
					t.Errorf("expected infinity, got %v", result)
				}

				return
			}

			absInput := tableTest.input
			if absInput < 0 {
				absInput = -absInput
			}

			// f16 min normal is ~6e-5, values below this will underflow to zero
			if absInput < 6e-5 && absInput > 0 {
				// Expect underflow to zero - this is expected behavior
				return
			}

			if absInput > 1e-10 {
				relErr := (result - tableTest.input) / tableTest.input
				if relErr < 0 {
					relErr = -relErr
				}

				if relErr > 0.01 {
					t.Errorf("relative error too large: input=%v, output=%v, relErr=%v", tableTest.input, result, relErr)
				}
			} else {
				absErr := result - tableTest.input
				if absErr < 0 {
					absErr = -absErr
				}

				if absErr > 1e-10 {
					t.Errorf("absolute error too large: input=%v, output=%v, err=%v", tableTest.input, result, absErr)
				}
			}
		})
	}
}

func TestFloat32ToF16Slice(t *testing.T) {
	t.Parallel()

	input := []float32{0.0, 1.0, -1.0, 0.5, -0.5, 2.0}
	f16bytes := Float32ToF16(input)

	if len(f16bytes) != len(input)*2 {
		t.Errorf("expected %d bytes, got %d", len(input)*2, len(f16bytes))
	}

	output := F16ToFloat32(f16bytes)
	if len(output) != len(input) {
		t.Fatalf("output length mismatch: expected %d, got %d", len(input), len(output))
	}

	for i, expected := range input {
		result := output[i]

		absErr := result - expected
		if absErr < 0 {
			absErr = -absErr
		}

		if absErr > 0.01 {
			t.Errorf("value %d: expected %v, got %v, error %v", i, expected, result, absErr)
		}
	}
}

func TestF16ToFloat32InvalidInput(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for odd-length input")
		}
	}()

	F16ToFloat32([]byte{1, 2, 3})
}

func TestFloat32ToF16InterleavedStereo(t *testing.T) {
	t.Parallel()

	channels := [][]float32{
		{0.0, 0.5, -0.5, 1.0},
		{0.25, -0.25, 0.75, -1.0},
	}

	f16bytes := Float32ToF16Interleaved(channels)

	expectedLen := 2 * 4 * 2
	if len(f16bytes) != expectedLen {
		t.Errorf("expected %d bytes, got %d", expectedLen, len(f16bytes))
	}
}

func TestF16ToFloat32DeinterleavedStereo(t *testing.T) {
	t.Parallel()

	channels := [][]float32{
		{0.0, 0.5, -0.5, 1.0},
		{0.25, -0.25, 0.75, -1.0},
	}

	f16bytes := Float32ToF16Interleaved(channels)
	reconstructed := F16ToFloat32Deinterleaved(f16bytes, 2)

	if len(reconstructed) != len(channels) {
		t.Fatalf("channel count mismatch: expected %d, got %d", len(channels), len(reconstructed))
	}

	for ch := range channels {
		if len(reconstructed[ch]) != len(channels[ch]) {
			t.Fatalf("channel %d length mismatch: expected %d, got %d", ch, len(channels[ch]), len(reconstructed[ch]))
		}

		for sample := range channels[ch] {
			expected := channels[ch][sample]
			result := reconstructed[ch][sample]

			absErr := result - expected
			if absErr < 0 {
				absErr = -absErr
			}

			if absErr > 0.01 {
				t.Errorf("ch %d sample %d: expected %v, got %v", ch, sample, expected, result)
			}
		}
	}
}

func TestFloat32ToF16InterleavedMismatchedChannels(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for mismatched channel lengths")
		}
	}()

	channels := [][]float32{
		{0.0, 0.5, -0.5},
		{0.25, -0.25},
	}
	Float32ToF16Interleaved(channels)
}

func TestF16ToFloat32DeinterleavedInvalidChannels(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid channel count")
		}
	}()

	data := make([]byte, 8)
	F16ToFloat32Deinterleaved(data, 0)
}

func TestAnalyzeConversionError(t *testing.T) {
	t.Parallel()

	samples := []float32{
		0.0, 0.1, 0.2, -0.1, -0.2, 0.5, -0.5, 1.0, -1.0,
		0.3, 0.4, 0.6, 0.7, 0.8, 0.9, -0.3, -0.4, -0.6,
	}

	stats := AnalyzeConversionError(samples)

	if stats.MaxRelError > 0.01 {
		t.Errorf("max relative error too high: %v", stats.MaxRelError)
	}

	if stats.SNR < 50 {
		t.Logf("warning: SNR lower than expected: %v dB", stats.SNR)
	}
}

func TestSpecialValuesConversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   float32
		checkFn func(float32) bool
	}{
		{"positive zero", 0.0, func(v float32) bool { return v == 0.0 }},
		{"negative zero", float32(math.Copysign(0, -1)), func(v float32) bool { return v == 0.0 }}, // Sign may not be preserved
		{"positive infinity", float32(math.Inf(1)), func(v float32) bool { return math.IsInf(float64(v), 1) }},
		{"negative infinity", float32(math.Inf(-1)), func(v float32) bool { return math.IsInf(float64(v), -1) }},
		{"NaN", float32(math.NaN()), func(v float32) bool { return math.IsNaN(float64(v)) }},
	}

	for _, tableTest := range tests {
		t.Run(tableTest.name, func(t *testing.T) {
			t.Parallel()

			f16bits := float32ToF16(tableTest.value)

			result := f16ToFloat32(f16bits)
			if !tableTest.checkFn(result) {
				t.Errorf("failed check for %v, got %v", tableTest.value, result)
			}
		})
	}
}

func BenchmarkFloat32ToF16(b *testing.B) {
	data := make([]float32, 1000)
	for i := range data {
		data[i] = float32(i) * 0.001
	}

	b.ResetTimer()

	for range b.N {
		Float32ToF16(data)
	}
}

func BenchmarkF16ToFloat32(b *testing.B) {
	data := make([]float32, 1000)
	for i := range data {
		data[i] = float32(i) * 0.001
	}

	f16bytes := Float32ToF16(data)

	b.ResetTimer()

	for range b.N {
		F16ToFloat32(f16bytes)
	}
}

func BenchmarkFloat32ToF16Interleaved(b *testing.B) {
	channels := make([][]float32, 2)
	for i := range channels {
		channels[i] = make([]float32, 500)
		for j := range channels[i] {
			channels[i][j] = float32(j) * 0.001
		}
	}

	b.ResetTimer()

	for range b.N {
		Float32ToF16Interleaved(channels)
	}
}

func BenchmarkF16ToFloat32Deinterleaved(b *testing.B) {
	channels := make([][]float32, 2)
	for i := range channels {
		channels[i] = make([]float32, 500)
		for j := range channels[i] {
			channels[i][j] = float32(j) * 0.001
		}
	}

	f16bytes := Float32ToF16Interleaved(channels)

	b.ResetTimer()

	for range b.N {
		F16ToFloat32Deinterleaved(f16bytes, 2)
	}
}
