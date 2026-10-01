package antigravity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/stretchr/testify/require"
)

func TestToolSignatureAndAttachmentsSurviveRoundTrip(t *testing.T) {
	t.Parallel()
	var response generateContentResponse
	require.NoError(t, json.Unmarshal([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"thoughtSignature":"c2lnbmF0dXJl","functionCall":{"id":"call-1","name":"read","args":{"path":"main.go"}}}]},"finishReason":"STOP"}]}`), &response))
	got, err := mapResponse(response, nil)
	require.NoError(t, err)
	var signature *google.ReasoningMetadata
	var tool fantasy.ToolCallContent
	for _, content := range got.Content {
		switch content := content.(type) {
		case fantasy.ReasoningContent:
			signature, _ = content.ProviderMetadata[google.Name].(*google.ReasoningMetadata)
		case fantasy.ToolCallContent:
			tool = content
		}
	}
	require.NotNil(t, signature)
	require.Equal(t, "signature", signature.Signature)
	require.Equal(t, "call-1", tool.ToolCallID)
	require.Equal(t, tool.ToolCallID, signature.ToolID)
	_, contents, warnings := toGeminiPrompt(fantasy.Prompt{
		{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.FilePart{MediaType: "image/png", Data: []byte("image")}}},
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
			fantasy.ReasoningPart{ProviderOptions: fantasy.ProviderOptions{google.Name: signature}},
			fantasy.ToolCallPart{ToolCallID: tool.ToolCallID, ToolName: tool.ToolName, Input: tool.Input},
		}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: tool.ToolCallID, Output: fantasy.ToolResultOutputContentText{Text: "source"}}}},
	})
	require.Empty(t, warnings)
	encoded, err := json.Marshal(contents)
	require.NoError(t, err)
	var wire []map[string]any
	require.NoError(t, json.Unmarshal(encoded, &wire))
	image := wire[0]["parts"].([]any)[0].(map[string]any)["inlineData"].(map[string]any)
	require.Equal(t, "image/png", image["mimeType"])
	require.Equal(t, "aW1hZ2U=", image["data"])
	call := wire[1]["parts"].([]any)[0].(map[string]any)
	require.Equal(t, "c2lnbmF0dXJl", call["thoughtSignature"])
	require.Equal(t, "call-1", call["functionCall"].(map[string]any)["id"])
	reply := wire[2]["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	require.Equal(t, "call-1", reply["id"])
}

func TestStreamPreservesSignedToolCall(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + `{"candidates":[{"content":{"parts":[{"thoughtSignature":"c2ln","functionCall":{"id":"tool-id","name":"read","args":{}}}]},"finishReason":"STOP"}]}` + "\n\n"))
	}))
	defer server.Close()
	provider, err := New(WithAPIKey("account-token"), WithProject("project"), WithBaseURL(server.URL))
	require.NoError(t, err)
	lm, err := provider.LanguageModel(t.Context(), "gemini-3.8-flash")
	require.NoError(t, err)
	stream, err := lm.Stream(t.Context(), fantasy.Call{})
	require.NoError(t, err)
	var signature *google.ReasoningMetadata
	var toolID string
	for part := range stream {
		require.NoError(t, part.Error)
		if part.Type == fantasy.StreamPartTypeReasoningEnd {
			signature, _ = part.ProviderMetadata[google.Name].(*google.ReasoningMetadata)
		}
		if part.Type == fantasy.StreamPartTypeToolCall {
			toolID = part.ID
		}
	}
	require.Equal(t, "tool-id", toolID)
	require.NotNil(t, signature)
	require.Equal(t, "sig", signature.Signature)
	require.Equal(t, toolID, signature.ToolID)
}
