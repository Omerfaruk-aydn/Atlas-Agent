// Package movavg computes moving averages over a series of values.
package movavg

// Simple returns the simple moving average of values over a sliding window.
// The result has len(values)-window+1 entries and entry i is the mean of
// values[i:i+window]. It returns nil when window is below 1 or larger than
// len(values).
func Simple(values []float64, window int) []float64 {
	if window < 1 || window > len(values) {
		return nil
	}
	out := make([]float64, 0, len(values)-window+1)
	for i := 0; i+window <= len(values); i++ {
		sum := 0.0
		for _, v := range values[i : i+window] {
			sum += v
		}
		out = append(out, sum/float64(window))
	}
	return out
}

// Exponential returns the exponential moving average of values with
// smoothing factor alpha, which must be greater than 0 and at most 1. The
// first entry equals values[0] and entry i is alpha*values[i] +
// (1-alpha)*out[i-1]. It returns nil for empty input or an invalid alpha.
func Exponential(values []float64, alpha float64) []float64 {
	if len(values) == 0 || alpha <= 0 || alpha > 1 {
		return nil
	}
	out := make([]float64, len(values))
	out[0] = values[0]
	for i := 1; i < len(values); i++ {
		out[i] = alpha*values[i] + (1-alpha)*out[i-1]
	}
	return out
}
