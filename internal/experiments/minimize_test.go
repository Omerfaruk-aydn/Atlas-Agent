package experiments

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMinimizePreservesMeasuredSignature(t *testing.T) {
	t.Parallel()
	result, err := Minimize(t.Context(), "noise\nFAULT\nmore\n", 3, "same failure", 32, func(ctx context.Context, input string) (Observation, error) {
		if strings.Contains(input, "FAULT") {
			return Observation{ExitCode: 3, Output: "same failure"}, nil
		}
		return Observation{ExitCode: 4, Output: "different failure"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "FAULT\n", result.Input)
	require.Equal(t, 3, result.Observation.ExitCode)
	require.LessOrEqual(t, result.Runs, 32)
}

func TestMinimizeRejectsNonReproducingBaseline(t *testing.T) {
	t.Parallel()
	_, err := Minimize(t.Context(), "input", 1, "failure", 4, func(context.Context, string) (Observation, error) {
		return Observation{}, nil
	})
	require.ErrorContains(t, err, "baseline")
}
