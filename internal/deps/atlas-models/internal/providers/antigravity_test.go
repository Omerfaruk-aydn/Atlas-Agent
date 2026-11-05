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
	// The gpt-oss-120b route retired on 2026-11-02, so the selectable
	// catalog omits it. Gemini 3.8 Flash is the documented replacement.
	require.NotContains(t, models, "gpt-oss-120b")
	flash := models["gemini-3.8-flash"]
	require.Equal(t, int64(1048576), flash.ContextWindow)
	require.Equal(t, int64(65536), flash.DefaultMaxTokens)
	require.True(t, flash.SupportsImages)
	require.True(t, flash.CanReason)
	require.Equal(t, "gemini-3.8-flash", entry.DefaultLargeModelID)
	require.Equal(t, "gemini-3.8-flash", entry.DefaultSmallModelID)
	require.Contains(t, models, entry.DefaultLargeModelID)
	require.Contains(t, models, entry.DefaultSmallModelID)
	// These older routes are not exposed by the verified Pro account.
	require.NotContains(t, models, "claude-sonnet-4-6")
	require.NotContains(t, models, "claude-opus-4-6")
}
