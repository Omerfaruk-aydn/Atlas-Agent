package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestResolveProviderOptionsReportsUnsupportedEffort(t *testing.T) {
	t.Parallel()

	model := Model{
		CatwalkCfg: catwalk.Model{ID: "m", CanReason: true, ReasoningLevels: []string{"low", "medium", "high"}, DefaultReasoningEffort: "medium"},
		ModelCfg:   config.SelectedModel{Provider: "anthropic", Model: "m", ReasoningEffort: "max"},
	}
	opts, adjustments := resolveProviderOptions(model, config.ProviderConfig{ID: "anthropic", Type: catwalk.TypeAnthropic})
	require.Len(t, adjustments, 1)
	require.Equal(t, "reasoning_effort", adjustments[0].Setting)
	require.Equal(t, "max", adjustments[0].Requested)
	require.Equal(t, "medium", adjustments[0].Applied)
	require.Contains(t, adjustments[0].Reason, "low, medium, high")
	// What is sent matches what is reported.
	parsed := opts[anthropic.Name].(*anthropic.ProviderOptions)
	require.NotNil(t, parsed.Effort)
	require.EqualValues(t, "medium", *parsed.Effort)
}

func TestCapabilitiesReportParsedEffortOverride(t *testing.T) {
	t.Parallel()
	m := Model{CatwalkCfg: catwalk.Model{ID: "m", CanReason: true, ReasoningLevels: []string{"low", "high"}}, ModelCfg: config.SelectedModel{Provider: "anthropic", Model: "m", ReasoningEffort: "low", ProviderOptions: map[string]any{"effort": "high"}}}
	caps := ResolveCapabilities(m, config.ProviderConfig{ID: "anthropic", Type: catwalk.TypeAnthropic})
	require.Equal(t, "high", caps.EffectiveEffort)
	require.Len(t, caps.Adjustments, 1)
	require.Contains(t, caps.Basis, "not live-verified")
	m.ModelCfg.ProviderOptions["effort"] = map[string]any{"invalid": true}
	caps = ResolveCapabilities(m, config.ProviderConfig{ID: "anthropic", Type: catwalk.TypeAnthropic})
	require.Empty(t, caps.EffectiveEffort, "a rejected option is never reported as sent")
}

func TestResolveProviderOptionsReportsSettingsForNonReasoningModel(t *testing.T) {
	t.Parallel()

	model := Model{
		CatwalkCfg: catwalk.Model{ID: "plain"},
		ModelCfg:   config.SelectedModel{Provider: "p", Model: "plain", ReasoningEffort: "high", Think: true},
	}
	_, adjustments := resolveProviderOptions(model, config.ProviderConfig{ID: "p", Type: catwalk.TypeOpenAI})
	require.Len(t, adjustments, 2)
	require.Equal(t, "none", adjustments[0].Applied)
	require.Equal(t, "think", adjustments[1].Setting)
}

// Options the adapter cannot parse used to be dropped without a trace,
// which also discarded every valid reasoning setting next to them.
func TestResolveProviderOptionsReportsRejectedOptions(t *testing.T) {
	t.Parallel()

	model := Model{
		CatwalkCfg: catwalk.Model{ID: "m", CanReason: true, ReasoningLevels: []string{"low"}},
		ModelCfg:   config.SelectedModel{Provider: "anthropic", Model: "m", ProviderOptions: map[string]any{"effort": map[string]any{"bad": true}}},
	}
	opts, adjustments := resolveProviderOptions(model, config.ProviderConfig{ID: "anthropic", Type: catwalk.TypeAnthropic})
	require.Empty(t, opts)
	require.Len(t, adjustments, 1)
	require.Equal(t, "provider_options", adjustments[0].Setting)
	require.Contains(t, adjustments[0].Reason, "anthropic adapter rejected")
}

func TestGoogleThinkingConfigOnlyForReasoningModels(t *testing.T) {
	t.Parallel()

	plain := Model{CatwalkCfg: catwalk.Model{ID: "gemini-3-image"}, ModelCfg: config.SelectedModel{Provider: "gemini", Model: "gemini-3-image"}}
	opts, adjustments := resolveProviderOptions(plain, config.ProviderConfig{ID: "gemini", Type: catwalk.TypeGoogle})
	require.Empty(t, adjustments)
	require.Nil(t, opts[google.Name].(*google.ProviderOptions).ThinkingConfig)

	reasoning := Model{CatwalkCfg: catwalk.Model{ID: "gemini-3-pro", CanReason: true, ReasoningLevels: []string{"low", "high"}, DefaultReasoningEffort: "high"}, ModelCfg: config.SelectedModel{Provider: "gemini", Model: "gemini-3-pro"}}
	opts, _ = resolveProviderOptions(reasoning, config.ProviderConfig{ID: "gemini", Type: catwalk.TypeGoogle})
	cfg := opts[google.Name].(*google.ProviderOptions).ThinkingConfig
	require.NotNil(t, cfg)
	require.Equal(t, "high", *cfg.ThinkingLevel)
}

func TestResolveCapabilitiesDescribesReplayAndAdapter(t *testing.T) {
	t.Parallel()

	model := Model{
		CatwalkCfg: catwalk.Model{ID: "claude-x", CanReason: true, ReasoningLevels: []string{"low", "high"}, ContextWindow: 1000, DefaultMaxTokens: 100, SupportsImages: true},
		ModelCfg:   config.SelectedModel{Provider: "vertex", Model: "claude-x", MaxTokens: 2000},
	}
	caps := ResolveCapabilities(model, config.ProviderConfig{ID: "vertex", Type: catwalk.TypeVertexAI})
	require.Equal(t, string(catwalk.TypeAnthropic), caps.Adapter)
	require.Equal(t, "signed thinking blocks, same model only", caps.ReasoningReplay)
	require.Equal(t, int64(2000), caps.MaxOutputTokens)
	require.Len(t, caps.Adjustments, 1)
	require.Equal(t, "max_tokens", caps.Adjustments[0].Setting)
}
