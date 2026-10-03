package experiments

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSummarizeDispersion(t *testing.T) {
	t.Parallel()
	values := []float64{30, 10, 20}
	result, err := Summarize(values)
	require.NoError(t, err)
	require.Equal(t, float64(20), result.Mean)
	require.Equal(t, float64(20), result.Median)
	require.Equal(t, float64(10), result.StdDev)
	require.Equal(t, []float64{30, 10, 20}, values)
}
