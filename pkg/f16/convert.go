// Package f16 provides IEEE 754 half-precision (float16) conversion utilities.
package f16

import (
	"encoding/binary"
	"math"
)

// Float32ToF16 converts a slice of float32 values to IEEE 754 half-precision (f16) bytes.
// Output is little-endian encoded, 2 bytes per value.
func Float32ToF16(values []float32) []byte {
	result := make([]byte, len(values)*2)
	for i, v := range values {
		binary.LittleEndian.PutUint16(result[i*2:], float32ToF16(v))
	}
	return result
}

// F16ToFloat32 converts a slice of IEEE 754 half-precision (f16) bytes to float32 values.
// Input must be little-endian encoded, 2 bytes per value.
func F16ToFloat32(data []byte) []float32 {
	if len(data)%2 != 0 {
		panic("F16ToFloat32: input length must be even")
	}
	result := make([]float32, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		bits := binary.LittleEndian.Uint16(data[i : i+2])
		result[i/2] = f16ToFloat32(bits)
	}
	return result
}

// Float32ToF16Interleaved converts a multi-channel float32 audio slice to f16 interleaved bytes.
// channels[i] represents the audio data for channel i (each sample slice contains samples)
// Output is interleaved: ch0_sample0, ch1_sample0, ch2_sample0, ch0_sample1, ch1_sample1, ...
func Float32ToF16Interleaved(channels [][]float32) []byte {
	if len(channels) == 0 {
		return []byte{}
	}

	numChannels := len(channels)
	numSamples := len(channels[0])

	// Verify all channels have the same length
	for i := 1; i < numChannels; i++ {
		if len(channels[i]) != numSamples {
			panic("Float32ToF16Interleaved: all channels must have equal length")
		}
	}

	result := make([]byte, numChannels*numSamples*2)
	idx := 0

	// Interleave channels
	for sample := 0; sample < numSamples; sample++ {
		for ch := 0; ch < numChannels; ch++ {
			binary.LittleEndian.PutUint16(result[idx:], float32ToF16(channels[ch][sample]))
			idx += 2
		}
	}

	return result
}

// F16ToFloat32Deinterleaved converts interleaved f16 bytes to deinterleaved multi-channel float32 audio.
// channels parameter specifies the number of audio channels.
// Output is a 2D slice where [channel][sample] contains the audio data.
func F16ToFloat32Deinterleaved(data []byte, channels int) [][]float32 {
	if len(data)%2 != 0 {
		panic("F16ToFloat32Deinterleaved: input length must be even")
	}
	if channels <= 0 {
		panic("F16ToFloat32Deinterleaved: channels must be > 0")
	}

	totalSamples := len(data) / 2
	if totalSamples%channels != 0 {
		panic("F16ToFloat32Deinterleaved: total samples must be divisible by channel count")
	}

	samplesPerChannel := totalSamples / channels
	result := make([][]float32, channels)
	for i := range result {
		result[i] = make([]float32, samplesPerChannel)
	}

	idx := 0
	for sample := 0; sample < samplesPerChannel; sample++ {
		for ch := 0; ch < channels; ch++ {
			bits := binary.LittleEndian.Uint16(data[idx : idx+2])
			result[ch][sample] = f16ToFloat32(bits)
			idx += 2
		}
	}

	return result
}

// float32ToF16 converts a single float32 value to IEEE 754 half-precision (16-bit) representation.
// Based on the IEEE 754 standard conversion algorithm.
func float32ToF16(value float32) uint16 {
	// Get the bit representation of the float32
	bits := math.Float32bits(value)

	// Extract sign (1 bit)
	sign := (bits >> 31) & 0x1

	// Extract exponent (8 bits)
	exponent := (bits >> 23) & 0xFF

	// Extract mantissa (23 bits)
	mantissa := bits & 0x7FFFFF

	// Handle special cases
	if exponent == 0xFF {
		// Infinity or NaN
		if mantissa == 0 {
			// Infinity
			return uint16((sign << 15) | 0x7C00)
		}
		// NaN - preserve quiet/signaling bit
		return uint16((sign << 15) | 0x7C00 | ((mantissa >> 13) & 0x3FF))
	}

	if exponent == 0 {
		// Zero or subnormal
		if mantissa == 0 {
			return uint16(sign << 15) // Signed zero
		}
		// Subnormal float32 -> denormalized float16 or zero
		// For now, flush to zero (can be improved)
		return uint16(sign << 15)
	}

	// Normalize exponent from float32 (bias 127) to float16 (bias 15)
	newExponent := int(exponent) - 127 + 15

	// Handle exponent overflow
	if newExponent >= 31 {
		// Overflow to infinity
		return uint16((sign << 15) | 0x7C00)
	}

	// Handle exponent underflow
	if newExponent <= 0 {
		// Underflow to zero or subnormal
		return uint16(sign << 15)
	}

	// Round mantissa from 23 bits to 10 bits
	// Shift right by 13 bits and apply rounding (round-to-nearest-even)
	roundedMantissa := (mantissa + 0x1000) >> 13

	// Check for mantissa overflow after rounding
	if roundedMantissa > 0x3FF {
		newExponent++
		roundedMantissa = 0
		if newExponent >= 31 {
			// Overflow to infinity
			return uint16((sign << 15) | 0x7C00)
		}
	}

	// Combine sign, exponent, and mantissa
	return uint16((sign << 15) | (uint16(newExponent) << 10) | (roundedMantissa & 0x3FF))
}

// f16ToFloat32 converts a single IEEE 754 half-precision (16-bit) value to float32.
func f16ToFloat32(bits uint16) float32 {
	// Extract components
	sign := uint32((bits >> 15) & 0x1)
	exponent := uint32((bits >> 10) & 0x1F)
	mantissa := uint32(bits & 0x3FF)

	// Handle special cases
	if exponent == 31 {
		if mantissa == 0 {
			// Infinity
			return math.Float32frombits((sign << 31) | 0x7F800000)
		}
		// NaN
		return math.Float32frombits((sign << 31) | 0x7FC00000 | (mantissa << 13))
	}

	if exponent == 0 {
		if mantissa == 0 {
			// Zero
			return math.Float32frombits(sign << 31)
		}
		// Denormalized (subnormal) float16
		// Convert to normalized float32
		exponent = 1
		// Keep mantissa as-is and normalize in float32 space
	}

	// Normalize exponent from float16 (bias 15) to float32 (bias 127)
	newExponent := exponent - 15 + 127

	// Shift mantissa from 10 bits to 23 bits (left by 13)
	newMantissa := mantissa << 13

	// Combine into float32 bit representation
	f32bits := (sign << 31) | (newExponent << 23) | newMantissa

	return math.Float32frombits(f32bits)
}

// Stats represents statistical information about float32->f16 conversion.
type Stats struct {
	MaxAbsError float32
	MaxRelError float32
	MeanError   float32
	SNR         float32 // Signal-to-Noise Ratio in dB
}

// AnalyzeConversionError calculates statistics on the conversion error.
// This is useful for validating f16 conversion quality for audio data.
func AnalyzeConversionError(original []float32) Stats {
	if len(original) == 0 {
		return Stats{}
	}

	// Convert to f16 and back
	f16Bytes := Float32ToF16(original)
	reconstructed := F16ToFloat32(f16Bytes)

	var maxAbsErr, maxRelErr, sumSqError float32
	var signalPower float32

	for i, orig := range original {
		error := reconstructed[i] - orig
		abserr := error
		if error < 0 {
			abserr = -error
		}

		if abserr > maxAbsErr {
			maxAbsErr = abserr
		}

		// Relative error (avoid division by very small numbers)
		absOrig := orig
		if orig < 0 {
			absOrig = -orig
		}
		if absOrig > 1e-10 {
			relerr := abserr / absOrig
			if relerr > maxRelErr {
				maxRelErr = relerr
			}
		}

		sumSqError += error * error
		signalPower += orig * orig
	}

	meanError := maxAbsErr / float32(len(original)) // Approximate

	// Calculate SNR: 10 * log10(signal_power / error_power)
	snr := float32(0)
	if sumSqError > 0 {
		noisePower := sumSqError / float32(len(original))
		signalPower = signalPower / float32(len(original))
		if signalPower > 0 {
			snr = 10 * float32(math.Log10(float64(signalPower/noisePower)))
		}
	}

	return Stats{
		MaxAbsError: maxAbsErr,
		MaxRelError: maxRelErr,
		MeanError:   meanError,
		SNR:         snr,
	}
}
