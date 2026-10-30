package model

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codereview"
	"github.com/stretchr/testify/require"
)

func TestParseReviewCommand(t *testing.T) {
	t.Parallel()

	req, ok, err := parseReviewCommand("/review")
	require.True(t, ok)
	require.NoError(t, err)
	require.Equal(t, reviewRequest{effort: codereview.EffortMedium}, req)

	req, ok, err = parseReviewCommand("  /review 123 high --comment ")
	require.True(t, ok)
	require.NoError(t, err)
	require.Equal(t, reviewRequest{spec: "123", effort: codereview.EffortHigh, comment: true}, req)

	req, _, err = parseReviewCommand("/review main..HEAD --effort max")
	require.NoError(t, err)
	require.Equal(t, reviewRequest{spec: "main..HEAD", effort: codereview.EffortMax}, req)

	req, _, err = parseReviewCommand("/review feature/x --effort=low")
	require.NoError(t, err)
	require.Equal(t, reviewRequest{spec: "feature/x", effort: codereview.EffortLow}, req)

	_, ok, _ = parseReviewCommand("/reviewer is great")
	require.False(t, ok, "only the whole word is the command")
	_, ok, _ = parseReviewCommand("please /review this")
	require.False(t, ok)

	_, ok, err = parseReviewCommand("/review --evil")
	require.True(t, ok)
	require.Error(t, err)
	_, _, err = parseReviewCommand("/review a b")
	require.Error(t, err)
	_, _, err = parseReviewCommand("/review --effort extreme")
	require.Error(t, err)
}
