package antigravity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapUsageKeepsCachedTokensOutOfInput(t *testing.T) {
	t.Parallel()
	usage := mapUsage(&usageMetadata{PromptTokenCount: 1000, CachedContentTokenCount: 800, CandidatesTokenCount: 20, TotalTokenCount: 1020})
	require.EqualValues(t, 200, usage.InputTokens)
	require.EqualValues(t, 800, usage.CacheReadTokens)
}
