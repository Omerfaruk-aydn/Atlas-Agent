package google

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// Gemini's promptTokenCount includes cached tokens; the shared Usage
// contract reports them only once, in CacheReadTokens.
func TestMapUsageKeepsCachedTokensOutOfInput(t *testing.T) {
	t.Parallel()
	usage := mapUsage(&genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1000, CachedContentTokenCount: 800, CandidatesTokenCount: 20, TotalTokenCount: 1020})
	require.EqualValues(t, 200, usage.InputTokens)
	require.EqualValues(t, 800, usage.CacheReadTokens)
	require.EqualValues(t, 1000, usage.InputTokens+usage.CacheReadTokens+usage.CacheCreationTokens)
}
