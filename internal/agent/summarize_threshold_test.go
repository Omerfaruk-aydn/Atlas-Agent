package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// An unknown context window must never trigger a summarize: cutting a
// session short on a guess is worse than letting the provider reject an
// oversized request.
func TestUnknownContextWindowNeverSummarizes(t *testing.T) {
	require.False(t, shouldAutoSummarize(0, 1_000_000, 0))
	require.False(t, shouldAutoSummarize(0, 1_000_000, 0.5))
	require.False(t, shouldAutoSummarize(-1, 1_000_000, 0))
}

func TestBuiltInThresholdForLargeWindows(t *testing.T) {
	const cw = 1_000_000 // above largeContextWindowThreshold: three quarters
	require.False(t, shouldAutoSummarize(cw, 749_999, 0))
	require.True(t, shouldAutoSummarize(cw, 750_000, 0), "a large window is summarized before it fills")

	const near = 210_000 // just above the threshold: three quarters is 157.5k
	require.False(t, shouldAutoSummarize(near, 157_000, 0))
	require.True(t, shouldAutoSummarize(near, 157_500, 0))
}

func TestBuiltInThresholdForSmallWindows(t *testing.T) {
	const cw = 100_000 // proportional: the last 20% of the window
	require.False(t, shouldAutoSummarize(cw, 79_999, 0))
	require.True(t, shouldAutoSummarize(cw, 80_000, 0))
}

func TestConfiguredRatioReplacesBothBuiltIns(t *testing.T) {
	// A large window, where the built-in would have waited until 980k.
	const cw = 1_000_000
	require.False(t, shouldAutoSummarize(cw, 499_999, 0.5))
	require.True(t, shouldAutoSummarize(cw, 500_000, 0.5))

	// A small one, where the built-in would have fired at 80k.
	const small = 100_000
	require.False(t, shouldAutoSummarize(small, 89_999, 0.9))
	require.True(t, shouldAutoSummarize(small, 90_000, 0.9))
}

// A ratio that can only misbehave -- summarizing every turn, or never --
// falls back to the built-in thresholds rather than being honoured.
func TestOutOfRangeRatioFallsBackToTheBuiltIns(t *testing.T) {
	const cw = 100_000
	for _, ratio := range []float64{0, -0.5, 1, 1.5} {
		require.False(t, shouldAutoSummarize(cw, 79_999, ratio), "ratio %v", ratio)
		require.True(t, shouldAutoSummarize(cw, 80_000, ratio), "ratio %v", ratio)
	}
}
