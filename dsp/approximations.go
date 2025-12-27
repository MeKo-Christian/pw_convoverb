package dsp

import "math"

// expApprox is a fast approximation of exp(x) for audio applications.
// Uses a polynomial approximation for better performance than math.Exp.
func expApprox(x float32) float32 {
	// For now, use standard library
	// TODO: Implement fast approximation if needed for performance
	return float32(math.Exp(float64(x)))
}

// log10Approx is a fast approximation of log10(x) for audio applications.
func log10Approx(x float32) float32 {
	// For now, use standard library
	// TODO: Implement fast approximation if needed for performance
	return float32(math.Log10(float64(x)))
}
