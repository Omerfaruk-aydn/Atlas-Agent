package antigravity

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// TestLiveThirdPartyAccount is opt-in and sends only synthetic test inputs.
// Set ATLAS_ANTIGRAVITY_LIVE=1, ATLAS_ANTIGRAVITY_TOKEN and
// ATLAS_ANTIGRAVITY_PROJECT in the test process. Credentials are never logged.
func TestLiveThirdPartyAccount(t *testing.T) {
	if os.Getenv("ATLAS_ANTIGRAVITY_LIVE") != "1" {
		t.Skip("Live account test is disabled")
	}
	token, project := os.Getenv("ATLAS_ANTIGRAVITY_TOKEN"), os.Getenv("ATLAS_ANTIGRAVITY_PROJECT")
	if token == "" || project == "" {
		t.Fatal("Live account credentials are required")
	}
	p, err := New(WithAPIKey(token), WithProject(project), WithHeaders(map[string]string{
		"User-Agent":      "antigravity/2.21.0 windows/amd64",
		"Client-Metadata": `{"ideType":"ANTIGRAVITY","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`,
	}))
	require.NoError(t, err)
	for _, tc := range []struct {
		model string
		limit int64
	}{
		{"claude-sonnet-5-5", 128000},
		{"claude-opus-5-5", 128000},
		{"gpt-oss-120b", 32768},
	} {
		t.Run(tc.model, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			m, err := p.LanguageModel(ctx, tc.model)
			require.NoError(t, err)
			call := accountToolCall()
			call.MaxOutputTokens = &tc.limit
			call.ProviderOptions = fantasy.ProviderOptions{Name: &ProviderOptions{ThinkingLevel: "medium"}}
			first, err := m.Generate(ctx, call)
			require.NoError(t, err)
			require.Equal(t, fantasy.FinishReasonToolCalls, first.FinishReason)
			require.Len(t, first.Content.ToolCalls(), 1)
			require.Equal(t, "echo", first.Content.ToolCalls()[0].ToolName)
			call.Prompt = appendToolReply(call.Prompt, first)
			stream, err := m.Stream(ctx, call)
			require.NoError(t, err)
			var text strings.Builder
			var finish fantasy.FinishReason
			for part := range stream {
				require.NoError(t, part.Error)
				switch part.Type {
				case fantasy.StreamPartTypeTextDelta:
					text.WriteString(part.Delta)
				case fantasy.StreamPartTypeToolCall:
					t.Error("The model repeated a completed tool call")
				case fantasy.StreamPartTypeFinish:
					finish = part.FinishReason
				}
			}
			require.Equal(t, "OK", strings.TrimSpace(text.String()))
			require.Equal(t, fantasy.FinishReasonStop, finish)
		})
	}
}
