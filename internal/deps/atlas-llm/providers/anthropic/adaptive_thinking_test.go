package anthropic

import (
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestCurrentClaudeAdaptiveThinking(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"claude-fable-5-1", "claude-mythos-5-1", "claude-opus-5-5", "claude-sonnet-5-5", "us.anthropic.claude-opus-5-5", "claude-fable-5-1@default"} {
		require.True(t, defaultsToAdaptiveThinking(model), model)
		require.True(t, requiresAdaptiveThinking(model), model)
	}
	require.False(t, defaultsToAdaptiveThinking("claude-haiku-4-5"))
	require.False(t, defaultsToAdaptiveThinking("MiniMax-M3"))
}

func TestMiniMaxM3ThinkingIsOptionalAndAdaptive(t *testing.T) {
	t.Parallel()
	lm := languageModel{modelID: "MiniMax-M3"}
	params, _, _, _, err := lm.prepareParams(fantasy.Call{})
	require.NoError(t, err)
	require.Nil(t, params.Thinking.OfAdaptive)
	params, _, _, _, err = lm.prepareParams(fantasy.Call{
		ProviderOptions: fantasy.ProviderOptions{Name: &ProviderOptions{Thinking: &ThinkingProviderOption{BudgetTokens: 2000}}},
	})
	require.NoError(t, err)
	require.NotNil(t, params.Thinking.OfAdaptive)
	require.Nil(t, params.Thinking.OfEnabled)
}

func TestAdaptiveThinkingOmitsSamplingParameters(t *testing.T) {
	t.Parallel()
	effort := Effort("high")
	for _, opts := range []*ProviderOptions{{}, {Effort: &effort}} {
		lm := languageModel{modelID: "claude-sonnet-5-5"}
		temperature, topP, topK := 0.7, 0.9, int64(20)
		params, _, warnings, _, err := lm.prepareParams(fantasy.Call{
			Temperature: &temperature, TopP: &topP, TopK: &topK,
			ProviderOptions: fantasy.ProviderOptions{Name: opts},
		})
		require.NoError(t, err)
		require.NotNil(t, params.Thinking.OfAdaptive)
		require.False(t, params.Temperature.Valid())
		require.False(t, params.TopP.Valid())
		require.False(t, params.TopK.Valid())
		require.Len(t, warnings, 3)
	}
}
