package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestGemini38RequestOmitsUnsupportedSamplingParameters(t *testing.T) {
	t.Parallel()
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}))
	defer server.Close()
	provider, err := New(WithGeminiAPIKey("test-key"), WithBaseURL(server.URL))
	require.NoError(t, err)
	lm, err := provider.LanguageModel(t.Context(), "gemini-3.8-flash")
	require.NoError(t, err)
	_, err = lm.Generate(t.Context(), fantasy.Call{
		Temperature: new(0.2), TopP: new(0.9), TopK: new(int64(10)),
		FrequencyPenalty: new(0.1), PresencePenalty: new(0.1),
		ProviderOptions: fantasy.ProviderOptions{Name: &ProviderOptions{ThinkingConfig: &ThinkingConfig{ThinkingLevel: new("medium")}}},
		Prompt:          fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Hello"}}}},
	})
	require.NoError(t, err)
	config, ok := (<-requests)["generationConfig"].(map[string]any)
	require.True(t, ok)
	for _, key := range []string{"temperature", "topP", "topK", "frequencyPenalty", "presencePenalty", "candidateCount"} {
		require.NotContains(t, config, key)
	}
	require.Equal(t, "medium", config["thinkingConfig"].(map[string]any)["thinkingLevel"])
}

func TestVertexGemini38PreservesToolCallIDs(t *testing.T) {
	t.Parallel()
	lm := languageModel{modelID: "gemini-3.8-flash", providerOptions: options{backend: genai.BackendVertexAI}}
	_, contents, _, err := lm.prepareParams(fantasy.Call{Prompt: fantasy.Prompt{
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ToolCallPart{ToolCallID: "call-1", ToolName: "read", Input: `{}`}}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: "call-1", Output: fantasy.ToolResultOutputContentText{Text: "source"}}}},
	}})
	require.NoError(t, err)
	require.Equal(t, "call-1", contents[0].Parts[0].FunctionCall.ID)
	require.Equal(t, "call-1", contents[1].Parts[0].FunctionResponse.ID)
}
