// Package resampler provides high-quality sample rate conversion.
package resampler

import (
	"math"
)

// Resampler performs sample rate conversion using windowed sinc interpolation.
type Resampler struct {
	// Quality parameter: number of sinc lobes on each side
	sincLobes int
}

// New creates a new Resampler instance with default quality.
func New() *Resampler {
	return &Resampler{
		sincLobes: 16, // Good balance of quality and speed
	}
}

// NewWithQuality creates a Resampler with specified quality.
// More lobes = higher quality but slower.
func NewWithQuality(lobes int) *Resampler {
	if lobes < 4 {
		lobes = 4
	}
	if lobes > 64 {
		lobes = 64
	}
	return &Resampler{
		sincLobes: lobes,
	}
}

// sinc computes sin(pi*x)/(pi*x) with proper handling at x=0.
func sinc(x float64) float64 {
	if math.Abs(x) < 1e-10 {
		return 1.0
	}
	pix := math.Pi * x
	return math.Sin(pix) / pix
}

// blackmanWindow computes the Blackman window value for a given position.
// x should be in range [-1, 1], returns 0 outside that range.
func blackmanWindow(x float64) float64 {
	if x < -1.0 || x > 1.0 {
		return 0.0
	}
	// Blackman window: 0.42 - 0.5*cos(2*pi*(x+1)/2) + 0.08*cos(4*pi*(x+1)/2)
	t := (x + 1.0) / 2.0 // Map [-1,1] to [0,1]
	return 0.42 - 0.5*math.Cos(2*math.Pi*t) + 0.08*math.Cos(4*math.Pi*t)
}

// Resample converts audio data from srcRate to dstRate using windowed sinc interpolation.
// Returns the resampled data as float32 slice.
func (r *Resampler) Resample(data []float32, srcRate, dstRate float64) ([]float32, error) {
	if len(data) == 0 {
		return []float32{}, nil
	}

	// No resampling needed if rates match
	if srcRate == dstRate {
		result := make([]float32, len(data))
		copy(result, data)
		return result, nil
	}

	ratio := dstRate / srcRate
	inputLen := len(data)
	outputLen := int(math.Round(float64(inputLen) * ratio))

	if outputLen == 0 {
		return []float32{}, nil
	}

	output := make([]float32, outputLen)

	// For each output sample, compute the windowed sinc interpolation
	for i := 0; i < outputLen; i++ {
		// Map output position to input position
		inputPos := float64(i) / ratio

		// Determine the filter width based on whether we're upsampling or downsampling
		filterRatio := 1.0
		if ratio < 1.0 {
			// Downsampling: widen the filter to avoid aliasing
			filterRatio = ratio
		}

		// Compute the interpolation window bounds
		windowRadius := float64(r.sincLobes) / filterRatio
		startIdx := int(math.Floor(inputPos - windowRadius))
		endIdx := int(math.Ceil(inputPos + windowRadius))

		// Clamp to input bounds
		if startIdx < 0 {
			startIdx = 0
		}
		if endIdx >= inputLen {
			endIdx = inputLen - 1
		}

		// Perform windowed sinc interpolation
		var sum float64
		var weightSum float64

		for j := startIdx; j <= endIdx; j++ {
			// Distance from the ideal input position
			d := inputPos - float64(j)

			// Apply the appropriate scaling for anti-aliasing
			scaledD := d * filterRatio

			// Sinc value
			s := sinc(scaledD)

			// Window value (normalized to filter width)
			w := blackmanWindow(d / windowRadius)

			// Combined weight
			weight := s * w

			sum += float64(data[j]) * weight
			weightSum += weight
		}

		// Normalize and apply anti-aliasing gain
		if weightSum > 0 {
			output[i] = float32(sum / weightSum)
		}
	}

	return output, nil
}

// ResampleMultiChannel resamples multi-channel audio data.
// Input: [channel][sample] at srcRate
// Output: [channel][sample] at dstRate
func (r *Resampler) ResampleMultiChannel(data [][]float32, srcRate, dstRate float64) ([][]float32, error) {
	if len(data) == 0 {
		return [][]float32{}, nil
	}

	result := make([][]float32, len(data))

	for ch := range data {
		resampled, err := r.Resample(data[ch], srcRate, dstRate)
		if err != nil {
			return nil, err
		}
		result[ch] = resampled
	}

	return result, nil
}

// CalculateOutputLength returns the expected output length for resampling.
func CalculateOutputLength(inputLen int, srcRate, dstRate float64) int {
	if inputLen == 0 {
		return 0
	}
	return int(math.Round(float64(inputLen) * dstRate / srcRate))
}
