package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEligibleAlternatives(t *testing.T) {
	t.Parallel()
	candidates, err := EligibleAlternatives([]AlternativeSummary{
		{ID: "a", Approach: "adapter", Regressions: 0},
		{ID: "b", Approach: "rewrite", Regressions: 2},
		{ID: "c", Approach: "patch", Regressions: 0},
	})
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "a", candidates[0].ID)
	_, err = EligibleAlternatives([]AlternativeSummary{{ID: "bad", Regressions: -1}})
	require.Error(t, err)
	_, err = EligibleAlternatives([]AlternativeSummary{{ID: "missing"}})
	require.Error(t, err)
}

func TestTournamentPromptBoundsDiff(t *testing.T) {
	t.Parallel()
	prompt := tournamentPrompt(AlternativeSummary{ID: "a", Approach: "a", Diff: strings.Repeat("x", 30000)}, AlternativeSummary{ID: "b", Approach: "b"})
	require.Contains(t, prompt, "[diff truncated]")
	require.Less(t, len(prompt), 26000)
}
