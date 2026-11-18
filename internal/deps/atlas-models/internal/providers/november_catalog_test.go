package providers

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

// novemberCatalog is the catalog as it is selectable on the day of the
// November 2026 update.
func novemberCatalog(t *testing.T, on time.Time) map[string]catwalk.Provider {
	t.Helper()
	all := map[string]catwalk.Provider{}
	for _, load := range providerRegistry {
		p := load().SelectableAt(on)
		all[string(p.ID)] = p
	}
	return all
}

func findModel(t *testing.T, all map[string]catwalk.Provider, provider, id string) catwalk.Model {
	t.Helper()
	for _, m := range all[provider].Models {
		if m.ID == id {
			return m
		}
	}
	require.Failf(t, "model missing", "%s/%s", provider, id)
	return catwalk.Model{}
}

func TestNovemberAnthropicFable52(t *testing.T) {
	t.Parallel()
	all := novemberCatalog(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	// Anthropic's model overview: $8/$40, 5m cache write $10, cache read
	// $0.20, 1M context, 128K output, adaptive thinking with default `high`.
	for provider, id := range map[string]string{
		"anthropic":      "claude-fable-5-2",
		"vertexai":       "claude-fable-5-2",
		"bedrock":        "global.anthropic.claude-fable-5-2",
		"bedrock-europe": "global.anthropic.claude-fable-5-2",
	} {
		m := findModel(t, all, provider, id)
		require.InDelta(t, 8, m.CostPer1MIn, 1e-9, provider)
		require.InDelta(t, 40, m.CostPer1MOut, 1e-9, provider)
		require.InDelta(t, 10, m.CostPer1MInCached, 1e-9, provider)
		require.InDelta(t, 0.2, m.CostPer1MOutCached, 1e-9, provider)
		require.Equal(t, int64(1000000), m.ContextWindow, provider)
		require.Equal(t, int64(128000), m.DefaultMaxTokens, provider)
		require.True(t, m.CanReason, provider)
		require.Equal(t, "high", m.DefaultReasoningEffort, provider)
	}
	mythos := findModel(t, all, "anthropic", "claude-mythos-5-2")
	require.Contains(t, mythos.Name, "Invite Only")
	require.InDelta(t, 8, mythos.CostPer1MIn, 1e-9)

	// The Claude subscription lists it without a per-token price.
	plan := findModel(t, all, "claude", "claude-fable-5-2")
	require.Zero(t, plan.CostPer1MIn)
	require.Zero(t, plan.CostPer1MOut)
	require.Equal(t, "claude-opus-5-5", all["claude"].DefaultLargeModelID, "the subscription default does not move")
	require.Equal(t, "claude-sonnet-5-5", all["anthropic"].DefaultLargeModelID)
}

func TestNovemberAnthropicSonnet45Retires(t *testing.T) {
	t.Parallel()
	before := novemberCatalog(t, time.Date(2026, 11, 29, 0, 0, 0, 0, time.UTC))
	findModel(t, before, "anthropic", "claude-sonnet-4-5-20250929")
	after := novemberCatalog(t, time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC))
	for _, m := range after["anthropic"].Models {
		require.NotEqual(t, "claude-sonnet-4-5-20250929", m.ID)
	}
}

func TestNovemberOpenAIGPT62(t *testing.T) {
	t.Parallel()
	all := novemberCatalog(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	// OpenAI's model pages: 1.05M context, 128K output; Astra $8/$40 with
	// $0.40 cached input and a $10 cache write; Sol $2/$10 with $0.10 and $2.50.
	astra := findModel(t, all, "openai", "gpt-6.2-astra")
	require.InDelta(t, 8, astra.CostPer1MIn, 1e-9)
	require.InDelta(t, 40, astra.CostPer1MOut, 1e-9)
	require.InDelta(t, 10, astra.CostPer1MInCached, 1e-9)
	require.InDelta(t, 0.4, astra.CostPer1MOutCached, 1e-9)
	sol := findModel(t, all, "openai", "gpt-6.2-sol")
	require.InDelta(t, 2, sol.CostPer1MIn, 1e-9)
	require.InDelta(t, 10, sol.CostPer1MOut, 1e-9)
	require.InDelta(t, 2.5, sol.CostPer1MInCached, 1e-9)
	require.InDelta(t, 0.1, sol.CostPer1MOutCached, 1e-9)
	for _, m := range []catwalk.Model{astra, sol} {
		require.Equal(t, int64(1050000), m.ContextWindow)
		require.Equal(t, int64(128000), m.DefaultMaxTokens)
		require.NotContains(t, m.ReasoningLevels, "none", "the API rejects reasoning effort none")
		require.Contains(t, m.ReasoningLevels, m.DefaultReasoningEffort)
	}
	require.Equal(t, "gpt-6.2-sol", all["openai"].DefaultLargeModelID)
	require.Equal(t, "gpt-6-luna", all["openai"].DefaultSmallModelID)

	// The ChatGPT (Codex) subscription and Copilot list both, unpriced.
	for _, provider := range []string{"chatgpt", "copilot"} {
		for _, id := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
			m := findModel(t, all, provider, id)
			require.Zero(t, m.CostPer1MIn, provider+"/"+id)
		}
	}
	require.Equal(t, "gpt-6.1-sol", all["chatgpt"].DefaultLargeModelID, "plan availability of 6.2 is not published, so the default stays")
}

func TestNovemberGemini4Family(t *testing.T) {
	t.Parallel()
	all := novemberCatalog(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	// Google's pricing page: Carbon and Argon launch at $2/$10 and rise to
	// $4/$20 on 2026-12-01; the catalog records the price that lasts.
	for _, id := range []string{"gemini-4-carbon", "gemini-4-argon"} {
		m := findModel(t, all, "gemini", id)
		require.InDelta(t, 4, m.CostPer1MIn, 1e-9, id)
		require.InDelta(t, 20, m.CostPer1MOut, 1e-9, id)
		require.InDelta(t, 0.2, m.CostPer1MOutCached, 1e-9, id)
		require.Equal(t, []string{"low", "medium", "high"}, m.ReasoningLevels, id)
		require.Equal(t, int64(1048576), m.ContextWindow, id)
		require.Equal(t, int64(65536), m.DefaultMaxTokens, id)
	}
	h := findModel(t, all, "gemini", "gemini-4-hydrogen")
	require.InDelta(t, 1.5, h.CostPer1MIn, 1e-9)
	require.InDelta(t, 7.5, h.CostPer1MOut, 1e-9)
	require.InDelta(t, 0.075, h.CostPer1MOutCached, 1e-9)
	require.Equal(t, []string{"low", "medium", "high", "xhigh"}, h.ReasoningLevels)
	require.Equal(t, "gemini-4-argon", all["gemini"].DefaultLargeModelID)

	for _, id := range []string{"gemini-4-carbon", "gemini-4-hydrogen", "gemini-4-argon"} {
		findModel(t, all, "copilot", id)
	}
	// Antigravity lists the three per Google's table; the adapter routes all
	// Gemini 4 models through the CLI client with the low/medium/high suffix.
	for _, id := range []string{"gemini-4-carbon", "gemini-4-hydrogen", "gemini-4-argon"} {
		findModel(t, all, "antigravity", id)
	}
}

func TestNovemberAntigravityGPTOSSRemoval(t *testing.T) {
	t.Parallel()
	has := func(all map[string]catwalk.Provider) bool {
		for _, m := range all["antigravity"].Models {
			if m.ID == "gpt-oss-120b" {
				return true
			}
		}
		return false
	}
	require.True(t, has(novemberCatalog(t, time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))))
	require.False(t, has(novemberCatalog(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))))
}

func TestNovemberOtherAdditions(t *testing.T) {
	t.Parallel()
	all := novemberCatalog(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	// Alibaba's international price page: $1.5/$4.5, 5m cache write $1.875,
	// cache hit $0.075.
	q := findModel(t, all, "alibaba-singapore", "qwen4-max")
	require.InDelta(t, 1.5, q.CostPer1MIn, 1e-9)
	require.InDelta(t, 4.5, q.CostPer1MOut, 1e-9)
	require.InDelta(t, 1.875, q.CostPer1MInCached, 1e-9)
	require.InDelta(t, 0.075, q.CostPer1MOutCached, 1e-9)
	require.Equal(t, "qwen3.8-max", all["alibaba-singapore"].DefaultLargeModelID)
	mai := findModel(t, all, "copilot", "mai-code-2")
	require.Equal(t, int64(1000000), mai.ContextWindow)
}

func TestNovemberDefaultsStayValid(t *testing.T) {
	t.Parallel()
	all := novemberCatalog(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	for _, provider := range []string{"anthropic", "claude", "openai", "chatgpt", "gemini", "vertexai", "bedrock", "bedrock-europe", "copilot", "antigravity", "alibaba-singapore"} {
		ids := map[string]bool{}
		for _, m := range all[provider].Models {
			require.False(t, ids[m.ID], "duplicate %s/%s", provider, m.ID)
			ids[m.ID] = true
		}
		require.True(t, ids[all[provider].DefaultLargeModelID], "%s default large", provider)
		require.True(t, ids[all[provider].DefaultSmallModelID], "%s default small", provider)
	}
}
