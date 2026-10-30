package providers

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

// The muse catalog entry is the model picker's only view of the
// Meta subscription: ids must match the models the Messages API
// serves, and every model must carry a max-tokens default because
// Meta requires max_tokens on every request.
func TestMuseCatalogEntry(t *testing.T) {
	var entry *catwalk.Provider
	for _, p := range GetAll() {
		if p.ID == catwalk.InferenceProviderMuse {
			p := p
			entry = &p
		}
	}
	require.NotNil(t, entry, "the embedded catalog must include the muse provider")

	require.Equal(t, "Muse", entry.Name)
	require.Equal(t, "https://api.meta.ai", entry.APIEndpoint)
	require.Equal(t, catwalk.TypeMuse, entry.Type)
	require.Equal(t, "muse-wonder-1.0", entry.DefaultLargeModelID)
	require.Equal(t, "muse-spark-1.2", entry.DefaultSmallModelID)

	ids := make(map[string]catwalk.Model, len(entry.Models))
	for _, m := range entry.Models {
		ids[m.ID] = m
	}
	for _, want := range []string{
		"muse-spark-1.3",
		"muse-spark-1.3-contributor",
		"muse-spark-1.2",
		"muse-spark-1.2-contributor",
		"muse-spark-1.1",
	} {
		m, ok := ids[want]
		require.True(t, ok, "catalog must include model %s", want)
		require.Positive(t, m.ContextWindow, "model %s needs a context window", want)
		require.Positive(t, m.DefaultMaxTokens, "model %s needs a max-tokens default; Meta rejects requests without max_tokens", want)
		require.True(t, m.CanReason, "Muse Spark always reasons; model %s must be marked accordingly", want)
		require.True(t, m.SupportsImages, "model %s supports image input", want)
	}
}

// kimi-code is the OAuth (membership) twin of the kimi-coding API-key
// provider: same models, but served over the OpenAI-compatible endpoint
// and with no api_key, since the OAuth access token is the bearer.
func TestKimiCodeCatalogEntry(t *testing.T) {
	byID := map[catwalk.InferenceProvider]catwalk.Provider{}
	for _, p := range GetAll() {
		byID[p.ID] = p
	}
	entry, ok := byID[catwalk.InferenceProviderKimiCode]
	require.True(t, ok, "the embedded catalog must include kimi-code")
	require.Equal(t, "Kimi Code (subscription)", entry.Name)
	require.Equal(t, catwalk.TypeOpenAICompat, entry.Type)
	require.Equal(t, "https://api.kimi.com/coding/v1", entry.APIEndpoint)
	require.Empty(t, entry.APIKey, "OAuth providers carry no static key")

	key, ok := byID[catwalk.InferenceKimiCoding]
	require.True(t, ok)
	require.Equal(t, catwalk.TypeAnthropic, key.Type, "the API-key provider is unchanged")
	require.NotEmpty(t, key.APIKey)

	want := make([]string, 0, len(key.Models))
	for _, m := range key.Models {
		want = append(want, m.ID)
	}
	got := make([]string, 0, len(entry.Models))
	for _, m := range entry.Models {
		got = append(got, m.ID)
	}
	require.Equal(t, want, got, "kimi-code mirrors the kimi-coding model list")
	require.Equal(t, key.DefaultLargeModelID, entry.DefaultLargeModelID)
	require.Equal(t, key.DefaultSmallModelID, entry.DefaultSmallModelID)
}

// minimax-plan and minimax-plan-cn are the OAuth (subscription) twins of
// minimax-coding: same Anthropic-compatible models, no static api_key, one
// account provider per region.
func TestMiniMaxPlanCatalogEntries(t *testing.T) {
	byID := map[catwalk.InferenceProvider]catwalk.Provider{}
	for _, p := range GetAll() {
		byID[p.ID] = p
	}
	key, ok := byID["minimax-coding"]
	require.True(t, ok)
	require.NotEmpty(t, key.APIKey, "the API-key provider is unchanged")

	for id, endpoint := range map[catwalk.InferenceProvider]string{
		catwalk.InferenceProviderMiniMaxPlan:   "https://api.minimax.io/anthropic",
		catwalk.InferenceProviderMiniMaxPlanCN: "https://api.minimax.cn/anthropic",
	} {
		entry, ok := byID[id]
		require.True(t, ok, "the embedded catalog must include %s", id)
		require.Equal(t, catwalk.TypeAnthropic, entry.Type, id)
		require.Equal(t, endpoint, entry.APIEndpoint, id)
		require.Empty(t, entry.APIKey, "OAuth providers carry no static key: %s", id)
		require.Contains(t, entry.Name, "(subscription)", id)

		var want, got []string
		for _, m := range key.Models {
			want = append(want, m.ID)
		}
		for _, m := range entry.Models {
			got = append(got, m.ID)
		}
		require.Equal(t, want, got, "%s mirrors the minimax-coding model list", id)
		require.Equal(t, key.DefaultLargeModelID, entry.DefaultLargeModelID, id)
		require.Equal(t, key.DefaultSmallModelID, entry.DefaultSmallModelID, id)
	}
}
