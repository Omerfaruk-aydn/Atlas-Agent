package harnessmetrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAblationCandidates(t *testing.T) {
	t.Parallel()
	gates := []GateSummary{
		{Name: "quiet", Triggered: 40, Passed: 40, DurationMS: 900},
		{Name: "useful", Triggered: 40, Passed: 30, SentBack: 10},
		{Name: "rare", Triggered: 5, Passed: 5},
		{Name: "flaky", Triggered: 30, Passed: 25, Unverified: 5},
	}
	got := AblationCandidates(gates)
	require.Len(t, got, 1)
	require.Equal(t, "quiet", got[0].Name)
	require.Contains(t, FormatAblation(gates), "quiet: ran 40 times")
	require.Empty(t, FormatAblation(gates[1:]))
}
