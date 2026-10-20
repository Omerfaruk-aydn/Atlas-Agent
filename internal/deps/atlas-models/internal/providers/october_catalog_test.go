package providers

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestOctoberModelAvailability(t *testing.T) {
	t.Parallel()
	all := map[string]catwalk.Provider{}
	for _, load := range providerRegistry {
		p := load().SelectableAt(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
		all[string(p.ID)] = p
	}
	for provider, id := range map[string]string{
		"anthropic": "claude-haiku-5-5", "gemini": "gemini-4-argon",
		"vertexai": "gemini-4-argon", "bedrock": "anthropic.claude-haiku-5-5",
		"antigravity": "gemini-4-argon", "minimax": "MiniMax-M3.1-Flash",
		"mistral": "mistral-large-4", "meta-api": "muse-wonder-1.0",
		"muse": "muse-wonder-full-1.0", "groq": "meta-models/muse-spark-open",
		"perplexity-agent": "openai/gpt-6.1-sol",
	} {
		ids := map[string]bool{}
		for _, m := range all[provider].Models {
			ids[m.ID] = true
		}
		require.True(t, ids[id], "%s/%s", provider, id)
	}
	for _, m := range all["anthropic"].Models {
		require.NotContains(t, []string{"claude-opus-4-20250514", "claude-opus-4-1-20250805", "claude-sonnet-4-20250514"}, m.ID)
	}
	for _, m := range all["copilot"].Models {
		require.NotContains(t, []string{"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5-mini", "gemini-3.7-flash"}, m.ID)
	}
	require.Equal(t, "gemini-3.8-flash", all["antigravity"].DefaultLargeModelID)
	require.Equal(t, "MiniMax-M3.1-Flash", all["minimax-m-plan"].DefaultLargeModelID)
	require.NotEmpty(t, catwalk.UnavailableProviderReason(catwalk.TypeOpenAICompat, "alibaba-token-plan-team-sgp"))
}

func TestSourcedLifecycleAndAgentRoutes(t *testing.T) {
	t.Parallel()
	for _, load := range providerRegistry {
		p := load()
		for _, m := range p.Models {
			if m.Lifecycle == nil {
				continue
			}
			require.Contains(t, []string{"retired", "deprecated", "alias"}, m.Lifecycle.Status)
			require.Contains(t, m.Lifecycle.SourceURL, "https://")
			_, err := time.Parse(time.DateOnly, m.Lifecycle.VerifiedAt)
			require.NoError(t, err, "%s/%s", p.ID, m.ID)
		}
	}
	p := perplexityAgentProvider()
	require.Equal(t, catwalk.TypeOpenAI, p.Type)
	require.Equal(t, "https://api.perplexity.ai/v1", p.APIEndpoint)
	require.Equal(t, "https://api.perplexity.ai", perplexityProvider().APIEndpoint, "legacy settings must not be silently rerouted")
}

func TestSavedCatalogCannotResurrectRetiredBuiltInModels(t *testing.T) {
	t.Parallel()
	p := catwalk.Provider{ID: "anthropic", DefaultLargeModelID: "claude-opus-4-20250514", Models: []catwalk.Model{
		{ID: "claude-opus-4-20250514"}, {ID: "claude-sonnet-5-5"},
	}}
	filtered := ApplyLifecycle(p, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	require.Len(t, filtered.Models, 1)
	require.Equal(t, "claude-sonnet-5-5", filtered.DefaultLargeModelID)
	require.Nil(t, p.Models[0].Lifecycle, "input must remain unchanged")
	p.ID = "my-private-host"
	require.Len(t, ApplyLifecycle(p, time.Now()).Models, 2)
}
