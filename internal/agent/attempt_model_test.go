package agent

import (
	"encoding/base64"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestRetryPreparationUsesFallbackCapabilities(t *testing.T) {
	t.Parallel()
	a := &sessionAgent{}
	initial := reasoningOrigin{Provider: "claude", Model: "first"}
	reasoning := fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ReasoningPart{Text: "primary reasoning"}}}
	origins := map[string]reasoningOrigin{}
	rememberReasoning(origins, []fantasy.Message{reasoning}, initial)
	call := fantasy.Call{Prompt: append([]fantasy.Message{reasoning}, mediaDeliveryMessages(viewportCaption, base64.StdEncoding.EncodeToString([]byte("image")))...), ProviderOptions: fantasy.ProviderOptions{anthropic.Name: &anthropic.ProviderOptions{}}}
	fallback := Model{ModelCfg: config.SelectedModel{Provider: "text-provider", Model: "second"}, CatwalkCfg: catwalk.Model{SupportsImages: false}}
	requested := int64(128000)
	call.MaxOutputTokens = &requested
	fallback.CatwalkCfg.DefaultMaxTokens = 4096
	out := a.prepareAttempt(call, fallback, initial, origins)
	require.Empty(t, reasoningParts(out.Prompt))
	require.Empty(t, out.ProviderOptions)
	require.EqualValues(t, 4096, *out.MaxOutputTokens)
	require.EqualValues(t, 128000, *call.MaxOutputTokens)
	media := out.Prompt[1].Content[0].(fantasy.ToolResultPart).Output.(fantasy.ToolResultOutputContentText)
	require.Contains(t, media.Text, "not supported")
	require.Contains(t, media.Text, viewportCaption)
	primary := Model{ModelCfg: config.SelectedModel{Provider: "claude", Model: "first"}, CatwalkCfg: catwalk.Model{SupportsImages: true}}
	out = a.prepareAttempt(call, primary, initial, origins)
	require.Len(t, reasoningParts(out.Prompt), 1)
	require.Equal(t, call.ProviderOptions, out.ProviderOptions)
	require.Len(t, reasoningParts(call.Prompt), 1)
}

func TestFallbackRemovesAlreadyConvertedImageAttachments(t *testing.T) {
	t.Parallel()
	a := &sessionAgent{}
	vision := Model{ModelCfg: config.SelectedModel{Provider: "openai"}, CatwalkCfg: catwalk.Model{SupportsImages: true}}
	messages := a.workaroundProviderMediaLimitations(mediaDeliveryMessages(viewportCaption, base64.StdEncoding.EncodeToString([]byte("image"))), vision)
	require.Len(t, messages, 2)
	textOnly := Model{ModelCfg: config.SelectedModel{Provider: "compatible"}}
	out := a.workaroundProviderMediaLimitations(messages, textOnly)
	for _, msg := range out {
		for _, part := range msg.Content {
			_, image := fantasy.AsMessagePart[fantasy.FilePart](part)
			require.False(t, image)
		}
	}
	require.Contains(t, out[0].Content[0].(fantasy.ToolResultPart).Output.(fantasy.ToolResultOutputContentText).Text, viewportCaption)
	require.Contains(t, out[1].Content[len(out[1].Content)-1].(fantasy.TextPart).Text, "does not support images")
	require.Len(t, messages[1].Content, 2, "input history remains intact")
}
