package providers

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestSelectableCatalogDefaults(t *testing.T) {
	t.Parallel()
	providerIDs := map[catwalk.InferenceProvider]bool{}
	for _, p := range GetAll() {
		require.False(t, providerIDs[p.ID], "%s appears more than once in provider selection", p.ID)
		providerIDs[p.ID] = true
		require.Empty(t, catwalk.UnavailableProviderReason(p.Type), p.ID)
		ids := map[string]bool{}
		for _, m := range p.Models {
			require.False(t, ids[m.ID], "%s/%s duplicated", p.ID, m.ID)
			ids[m.ID] = true
		}
		if p.DefaultLargeModelID != "" {
			require.True(t, ids[p.DefaultLargeModelID], "%s large default missing", p.ID)
		}
		if p.DefaultSmallModelID != "" {
			require.True(t, ids[p.DefaultSmallModelID], "%s small default missing", p.ID)
		}
	}
}

func TestCorrectedProviderContracts(t *testing.T) {
	t.Parallel()
	providers := map[string]catwalk.Provider{}
	for _, p := range GetAll() {
		providers[string(p.ID)] = p
	}
	require.Equal(t, "https://api.cohere.ai/compatibility/v1", providers["cohere"].APIEndpoint)
	require.Equal(t, "openai/gpt-oss-120b", providers["groq"].DefaultLargeModelID)
	require.Equal(t, "mistral-medium-3-5", providers["mistral"].DefaultLargeModelID)
	require.NotEqual(t, providers["zai"].APIEndpoint, providers["zai-api"].APIEndpoint)
	require.Equal(t, "https://coding-intl.dashscope.aliyuncs.com/v1", providers["alibaba-coding"].APIEndpoint)
	require.Len(t, providers["alibaba-coding"].Models, 10)
	for _, m := range providers["alibaba-coding"].Models {
		require.NotContains(t, m.ID, "qwen3.8", "API availability does not imply plan availability")
		require.Zero(t, m.CostPer1MIn)
	}
	require.Equal(t, "MiniMax-M3.1-Flash-Preview", providers["minimax-m-plan"].DefaultLargeModelID)
	require.NotEqual(t, providers["minimax-m-plan"].APIKey, providers["minimax-coding"].APIKey)
}

func TestUnverifiedAndDuplicateConnectionsAreNotSelectable(t *testing.T) {
	t.Parallel()
	for _, p := range GetAll() {
		require.NotContains(t, []catwalk.InferenceProvider{"amp", "bolt", "phind", "codex-ide"}, p.ID)
	}
}
