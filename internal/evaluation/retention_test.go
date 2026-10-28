package evaluation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetentionDetectsMissingConstraintsAndFalseVerification(t *testing.T) {
	t.Parallel()
	cases, err := RetentionCases()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(cases), 3)
	for _, c := range cases {
		require.Greater(t, len(c.Transcript), 4000)
		good := ScoreRetention(c, c.Summary)
		require.Equal(t, len(c.Questions), good.Passed, c.ID)
		require.Equal(t, len(c.Questions), ScoreRetention(c, c.Handoff).Passed, c.ID)
		lost := ScoreRetention(c, "Work completed. Tests pass.")
		require.Zero(t, lost.Passed)
		require.NotEmpty(t, lost.Missing)
	}
	contradictory := ScoreRetention(cases[0], cases[0].Summary+"\nAll tests passed.")
	require.Contains(t, contradictory.Missing, "check")
}
