package antigravity

import (
	"encoding/base64"
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
	require.Equal(t, "b64:c2lnbmF0dXJl", signature.Signature, "signatures are kept as base64 text")
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
	raw, ok := google.DecodeSignature(signature.Signature)
	require.True(t, ok)
	require.Equal(t, "sig", string(raw))
	require.Equal(t, toolID, signature.ToolID)
}

// A real signature is binary. Saving it with the message as JSON and
// reading it back must send Gemini the same bytes, and each tool call must
// get its own signature.
func TestBinarySignatureSurvivesSavingTheMessage(t *testing.T) {
	t.Parallel()
	raw := []byte{0x0a, 0xff, 0xfe, 0x80, 0x12, 0x00, 0xc3}
	encoded := base64.StdEncoding.EncodeToString(raw)
	var response generateContentResponse
	require.NoError(t, json.Unmarshal([]byte(`{"candidates":[{"content":{"role":"model","parts":[`+
		`{"text":"thinking","thought":true,"thoughtSignature":"`+encoded+`"},`+
		`{"thoughtSignature":"`+encoded+`","functionCall":{"id":"call-1","name":"read","args":{}}},`+
		`{"functionCall":{"id":"call-2","name":"read","args":{}}}]},"finishReason":"STOP"}]}`), &response))
	got, err := mapResponse(response, nil)
	require.NoError(t, err)

	var parts []fantasy.MessagePart
	for _, content := range got.Content {
		switch content := content.(type) {
		case fantasy.ReasoningContent:
			metadata := content.ProviderMetadata[google.Name].(*google.ReasoningMetadata)
			// Round-trip the signature through JSON, as saving the message
			// does; raw bytes would come back with U+FFFD in them.
			saved, err := json.Marshal(metadata.Signature)
			require.NoError(t, err)
			loaded := google.ReasoningMetadata{ToolID: metadata.ToolID}
			require.NoError(t, json.Unmarshal(saved, &loaded.Signature))
			parts = append(parts, fantasy.ReasoningPart{Text: content.Text, ProviderOptions: fantasy.ProviderOptions{google.Name: &loaded}})
		case fantasy.ToolCallContent:
			parts = append(parts, fantasy.ToolCallPart{ToolCallID: content.ToolCallID, ToolName: content.ToolName, Input: content.Input})
		}
	}
	_, contents, _ := toGeminiPrompt(fantasy.Prompt{{Role: fantasy.MessageRoleAssistant, Content: parts}})
	wire, err := json.Marshal(contents)
	require.NoError(t, err)
	var sent []map[string]any
	require.NoError(t, json.Unmarshal(wire, &sent))
	model := sent[0]["parts"].([]any)
	require.Len(t, model, 3)
	require.Equal(t, encoded, model[0].(map[string]any)["thoughtSignature"])
	require.Equal(t, encoded, model[1].(map[string]any)["thoughtSignature"])
	require.NotContains(t, model[2].(map[string]any), "thoughtSignature", "only the first call carried a signature")
}

// A signature saved before the base64 encoding lost its bytes; it is not
// sent as is, which Gemini rejects as corrupted.
func TestCorruptedLegacySignatureIsNotSent(t *testing.T) {
	t.Parallel()
	// What JSON made of binary bytes: invalid UTF-8 became U+FFFD.
	legacy := &google.ReasoningMetadata{Signature: "\n��", ToolID: "call-1"}
	thought := &google.ReasoningMetadata{Signature: legacy.Signature}
	_, contents, _ := toGeminiPrompt(fantasy.Prompt{{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
		fantasy.ReasoningPart{Text: "thinking", ProviderOptions: fantasy.ProviderOptions{google.Name: thought}},
		fantasy.ReasoningPart{ProviderOptions: fantasy.ProviderOptions{google.Name: legacy}},
		fantasy.ToolCallPart{ToolCallID: "call-1", ToolName: "read", Input: "{}"},
	}}})
	require.Empty(t, contents[0].Parts[0].ThoughtSignature, "a lost thought signature is left out")
	skip, err := base64.URLEncoding.DecodeString("skip_thought_signature_validator")
	require.NoError(t, err)
	require.Equal(t, skip, contents[0].Parts[1].ThoughtSignature, "a function call gets the skip value")
}
