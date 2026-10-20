package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openaicompat"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestOctoberResponsesGatewaysPreserveEffort(t *testing.T) {
	for _, test := range []struct{ provider, model string }{
		{"perplexity-agent", "openai/gpt-6.1-sol"},
		{"perplexity-agent", "anthropic/claude-haiku-5-5"},
		{"perplexity-agent", "google/gemini-4-argon"},
		{"meta-api", "muse-wonder-1.0"},
	} {
		t.Run(test.provider+"/"+test.model, func(t *testing.T) {
			coord := hermeticSubagentCoordinator(t)
			requests := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/responses" {
					http.Error(w, "wrong protocol", http.StatusBadRequest)
					return
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				requests <- body
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp_test","object":"response","status":"completed","output":[{"type":"message","id":"msg_test","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
			}))
			defer server.Close()
			pc := config.ProviderConfig{ID: test.provider, Type: catwalk.TypeOpenAI, APIKey: "test-key", BaseURL: server.URL + "/v1"}
			provider, err := coord.buildProvider(pc, config.SelectedModel{Model: test.model}, false)
			require.NoError(t, err)
			lm, err := provider.LanguageModel(t.Context(), test.model)
			require.NoError(t, err)
			model := Model{CatwalkCfg: catwalk.Model{ID: test.model, CanReason: true, ReasoningLevels: []string{"low", "high"}}, ModelCfg: config.SelectedModel{ReasoningEffort: "low"}}
			_, err = lm.Generate(t.Context(), fantasy.Call{
				MaxOutputTokens: new(int64(4096)), Temperature: new(0.2),
				ProviderOptions: getProviderOptions(model, pc),
				Prompt:          fantasy.Prompt{fantasy.NewUserMessage("Hello")},
			})
			require.NoError(t, err)
			body := <-requests
			require.Equal(t, test.model, body["model"])
			require.Equal(t, float64(4096), body["max_output_tokens"])
			require.Equal(t, "low", body["reasoning"].(map[string]any)["effort"])
			require.NotContains(t, body, "temperature")
			if test.provider == "perplexity-agent" {
				require.NotContains(t, body, "include", "OpenAI-only encrypted reasoning must not be assumed on this gateway")
			}
		})
	}
}

func TestMiniMax31GatewayDoesNotDropSelectedEffort(t *testing.T) {
	t.Parallel()
	model := Model{CatwalkCfg: catwalk.Model{
		ID: "minimax-m3.1-flash-free", CanReason: true,
		ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"},
	}, ModelCfg: config.SelectedModel{ReasoningEffort: "low"}}
	options := getProviderOptions(model, config.ProviderConfig{ID: "opencode-zen", Type: catwalk.TypeOpenAICompat, OpenCodeTransport: "api"})
	compat, ok := options[openaicompat.Name].(*openaicompat.ProviderOptions)
	require.True(t, ok)
	require.NotNil(t, compat.ReasoningEffort)
	require.Equal(t, "low", string(*compat.ReasoningEffort))
}
