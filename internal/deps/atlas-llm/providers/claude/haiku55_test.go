package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/stretchr/testify/require"
)

func TestHaiku55AccountEffortAndLegacyThinking(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"", "low", "medium", "high", "xhigh", "max", "legacy-budget"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var wire struct {
					Model    string `json:"model"`
					Thinking struct {
						Type    string `json:"type"`
						Display string `json:"display"`
						Budget  int    `json:"budget_tokens"`
					} `json:"thinking"`
					Output struct{ Effort string } `json:"output_config"`
				}
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if wire.Model != "claude-haiku-5-5" || wire.Thinking.Type != "adaptive" || wire.Thinking.Budget != 0 || wire.Thinking.Display != "summarized" {
					t.Errorf("Haiku 5.5 request used the wrong thinking configuration: %+v", wire)
				}
				if level != "" && level != "legacy-budget" && wire.Output.Effort != level {
					t.Errorf("Expected effort %s, got %s", level, wire.Output.Effort)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"msg-test","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
			}))
			defer server.Close()
			p, err := New(WithAccessToken("test-token"), WithBaseURL(server.URL))
			require.NoError(t, err)
			m, err := p.LanguageModel(t.Context(), "claude-haiku-5-5")
			require.NoError(t, err)
			opts := &anthropic.ProviderOptions{}
			if level == "legacy-budget" {
				opts.Thinking = &anthropic.ThinkingProviderOption{BudgetTokens: 2000}
			} else if level != "" {
				effort := anthropic.Effort(level)
				opts.Effort = &effort
			}
			response, err := m.Generate(t.Context(), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("Say OK")}, ProviderOptions: fantasy.ProviderOptions{anthropic.Name: opts}})
			require.NoError(t, err)
			require.Equal(t, "OK", response.Content.Text())
		})
	}
}

// TestLiveHaiku55Account is opt-in and sends only synthetic inputs.
// Credentials are read from the test process and never logged.
func TestLiveHaiku55Account(t *testing.T) {
	if os.Getenv("ATLAS_CLAUDE_LIVE") != "1" {
		t.Skip("Live account test is disabled")
	}
	token := os.Getenv("ATLAS_CLAUDE_TOKEN")
	if token == "" {
		t.Fatal("Live account token is required")
	}
	p, err := New(WithAccessToken(token), WithAccountID(os.Getenv("ATLAS_CLAUDE_ACCOUNT")))
	require.NoError(t, err)
	m, err := p.LanguageModel(t.Context(), "claude-haiku-5-5")
	require.NoError(t, err)
	for _, level := range []anthropic.Effort{anthropic.EffortLow, anthropic.EffortMedium, anthropic.EffortHigh, anthropic.EffortXHigh, anthropic.EffortMax} {
		t.Run(string(level), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			limit := int64(2048)
			call := fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("Synthetic integration test. Respond with OK only.")}, MaxOutputTokens: &limit, ProviderOptions: fantasy.ProviderOptions{anthropic.Name: &anthropic.ProviderOptions{Effort: &level}}}
			stream, err := m.Stream(ctx, call)
			require.NoError(t, err)
			var text strings.Builder
			var finish fantasy.FinishReason
			for part := range stream {
				require.NoError(t, part.Error)
				if part.Type == fantasy.StreamPartTypeTextDelta {
					text.WriteString(part.Delta)
				}
				if part.Type == fantasy.StreamPartTypeFinish {
					finish = part.FinishReason
				}
			}
			require.Equal(t, "OK", strings.TrimSpace(text.String()))
			require.Equal(t, fantasy.FinishReasonStop, finish)
		})
	}
}
