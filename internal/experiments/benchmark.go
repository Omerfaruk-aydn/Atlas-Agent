package experiments

import (
	"fmt"
	"math"
	"slices"
)

type Statistics struct {
	Samples int     `json:"samples"`
	Mean    float64 `json:"mean_ms"`
	Median  float64 `json:"median_ms"`
	StdDev  float64 `json:"stddev_ms"`
	Minimum float64 `json:"minimum_ms"`
	Maximum float64 `json:"maximum_ms"`
}

// Summarize reports dispersion instead of inferring significance from one run.
func Summarize(values []float64) (Statistics, error) {
	if len(values) < 3 || len(values) > 20 {
		return Statistics{}, fmt.Errorf("benchmark requires 3-20 samples")
	}
	copyValues := slices.Clone(values)
	s := Statistics{Samples: len(values)}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return Statistics{}, fmt.Errorf("benchmark sample must be finite and nonnegative")
		}
		s.Mean += value / float64(len(values))
	}
	slices.Sort(copyValues)
	s.Minimum, s.Maximum = copyValues[0], copyValues[len(values)-1]
	middle := len(values) / 2
	s.Median = copyValues[middle]
	if len(values)%2 == 0 {
		s.Median = (copyValues[middle-1] + copyValues[middle]) / 2
	}
	for _, value := range values {
		s.StdDev += (value - s.Mean) * (value - s.Mean) / float64(len(values)-1)
	}
	s.StdDev = math.Sqrt(s.StdDev)
	return s, nil
}
