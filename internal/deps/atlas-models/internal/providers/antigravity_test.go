package providers

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestAntigravityAccountCatalog(t *testing.T) {
	t.Parallel()
	var entry catwalk.Provider
	for _, p := range GetAll() {
		if p.ID == catwalk.InferenceProviderAntigravity {
			entry = p
			break
		}
	}
	require.Equal(t, catwalk.TypeAntigravity, entry.Type)
	require.Equal(t, "https://daily-cloudcode-pa.googleapis.com", entry.APIEndpoint)
	models := make(map[string]catwalk.Model)
	for _, model := range entry.Models {
		require.NotContains(t, models, model.ID, "Duplicate selectable model")
		models[model.ID] = model
	}
	for _, id := range []string{"claude-sonnet-5-5", "claude-opus-5-5"} {
		model := models[id]
		require.Contains(t, models, id)
		require.Equal(t, int64(1000000), model.ContextWindow)
		require.Equal(t, int64(128000), model.DefaultMaxTokens)
		require.True(t, model.SupportsImages)
		require.True(t, model.CanReason)
		require.Equal(t, []string{"low", "medium", "high"}, model.ReasoningLevels)
		require.Equal(t, "medium", model.DefaultReasoningEffort)
		require.Zero(t, model.CostPer1MIn)
		require.Zero(t, model.CostPer1MOut)
	}
	oss := models["gpt-oss-120b"]
	require.Equal(t, int64(131072), oss.ContextWindow)
	require.Equal(t, int64(32768), oss.DefaultMaxTokens)
	require.False(t, oss.SupportsImages)
	require.True(t, oss.CanReason)
	require.Equal(t, []string{"medium"}, oss.ReasoningLevels)
	require.Equal(t, "medium", oss.DefaultReasoningEffort)
	require.Contains(t, models, entry.DefaultLargeModelID)
	require.Contains(t, models, entry.DefaultSmallModelID)
	// These older routes are not exposed by the verified Pro account.
	require.NotContains(t, models, "claude-sonnet-4-6")
	require.NotContains(t, models, "claude-opus-4-6")
}
