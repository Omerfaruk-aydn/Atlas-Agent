package agent

import (
	"context"
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

func TestXiaomiToolHistoryRequest(t *testing.T) {
	t.Parallel()
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-plan-key" {
			http.Error(w, "incorrect path or authorization", http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"response-1","object":"chat.completion","model":"mimo-v2.6-pro","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}))
	defer server.Close()
	provider, err := openaicompat.New(openaicompat.WithBaseURL(server.URL+"/v1"), openaicompat.WithAPIKey("test-plan-key"))
	require.NoError(t, err)
	lm, err := provider.LanguageModel(context.Background(), "mimo-v2.6-pro")
	require.NoError(t, err)
	model := Model{
		CatwalkCfg: catwalk.Model{ID: "mimo-v2.6-pro", CanReason: true},
		ModelCfg:   config.SelectedModel{Think: true},
	}
	maxTokens := int64(4096)
	_, err = lm.Generate(context.Background(), fantasy.Call{
		MaxOutputTokens: &maxTokens,
		ProviderOptions: getProviderOptions(model, config.ProviderConfig{ID: "xiaomi-token-plan-ams", Type: catwalk.TypeOpenAICompat}),
		Prompt: fantasy.Prompt{
			{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Read the file"}}},
			{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
				fantasy.ReasoningPart{Text: "I need the file contents before answering."},
				fantasy.ToolCallPart{ToolCallID: "call-1", ToolName: "view", Input: `{"path":"main.go"}`},
			}},
			{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
				fantasy.ToolResultPart{ToolCallID: "call-1", Output: fantasy.ToolResultOutputContentText{Text: "package main"}},
			}},
		},
	})
	require.NoError(t, err)
	request := <-requests
	require.Equal(t, "mimo-v2.6-pro", request["model"])
	require.Equal(t, "enabled", request["thinking"].(map[string]any)["type"])
	require.NotContains(t, request, "reasoning_effort")
	messages := request["messages"].([]any)
	assistant := messages[1].(map[string]any)
	require.Equal(t, "I need the file contents before answering.", assistant["reasoning_content"], "MiMo rejects tool history without the original reasoning")
	require.Contains(t, assistant, "tool_calls")
}
