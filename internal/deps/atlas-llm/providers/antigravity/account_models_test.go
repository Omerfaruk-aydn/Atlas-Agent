package antigravity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestThirdPartyAccountRoutes(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"claude-sonnet-5-5", "claude-opus-5-5", "gpt-oss-120b"} {
		for _, effort := range []string{"", "low", "medium", "high", "max"} {
			t.Run(model+"/"+effort, func(t *testing.T) {
				t.Parallel()
				m := &languageModel{modelID: model}
				env, _, err := m.buildEnvelope(fantasy.Call{ProviderOptions: fantasy.ProviderOptions{Name: &ProviderOptions{ThinkingLevel: effort}}})
				if effort == "max" || (model == "gpt-oss-120b" && effort != "" && effort != "medium") {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				want := effort
				if want == "" {
					want = "medium"
				}
				require.Equal(t, model+"-"+want, env.Model)
				if env.Request.GenerationConfig != nil {
					require.Nil(t, env.Request.GenerationConfig.ThinkingConfig)
				}
			})
		}
	}
}

func TestThirdPartyAccountInputLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		model string
		limit int64
	}{
		{"claude-sonnet-5-5", 128000},
		{"claude-opus-5-5", 128000},
		{"gpt-oss-120b", 32768},
	} {
		t.Run(tc.model, func(t *testing.T) {
			t.Parallel()
			m := &languageModel{modelID: tc.model}
			for _, tokens := range []int64{1, tc.limit, 0, -1, tc.limit + 1} {
				env, _, err := m.buildEnvelope(fantasy.Call{MaxOutputTokens: &tokens})
				if tokens < 1 || tokens > tc.limit {
					require.ErrorContains(t, err, "max output tokens")
					continue
				}
				require.NoError(t, err)
				require.EqualValues(t, tokens, env.Request.GenerationConfig.MaxOutputTokens)
			}
			_, _, err := m.buildEnvelope(fantasy.Call{Prompt: fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.FilePart{MediaType: "image/png", Data: []byte("image")}}}}})
			if tc.model == "gpt-oss-120b" {
				require.ErrorContains(t, err, "does not support attachments")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestExplicitAccountRoutes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ model, effort, want string }{
		{"claude-sonnet-5-5-high", "", "claude-sonnet-5-5-high"},
		{"claude-opus-5-5-low", "high", "claude-opus-5-5-high"},
		{"gpt-oss-120b-medium", "", "gpt-oss-120b-medium"},
		{"gemini-3.8-flash-medium", "low", "gemini-3.8-flash-low"},
	} {
		env, _, err := (&languageModel{modelID: tc.model}).buildEnvelope(fantasy.Call{ProviderOptions: fantasy.ProviderOptions{Name: &ProviderOptions{ThinkingLevel: tc.effort}}})
		require.NoError(t, err)
		require.Equal(t, tc.want, env.Model)
		if env.Request.GenerationConfig != nil {
			require.Nil(t, env.Request.GenerationConfig.ThinkingConfig)
		}
	}
	_, _, err := resolveModel("gpt-oss-120b-high", "")
	require.ErrorContains(t, err, "only medium")
	got, routed, err := resolveModel("custom-model-high", "low")
	require.NoError(t, err)
	require.False(t, routed)
	require.Equal(t, "custom-model-high", got)
}

func TestThirdPartyToolConversation(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"claude-sonnet-5-5", "claude-opus-5-5", "gpt-oss-120b"} {
		t.Run(model, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var wire envelope
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if wire.Model != model+"-medium" {
					t.Errorf("Unexpected account route: %s", wire.Model)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if len(wire.Request.Contents) == 1 {
					_, _ = w.Write([]byte("data: " + `{"response":{"candidates":[{"content":{"parts":[{"thought":true,"text":"Use echo","thoughtSignature":"c2ln"},{"functionCall":{"id":"call-1","name":"echo","args":{"value":"OK"}}}]},"finishReason":"STOP"}]}}` + "\n\n"))
					return
				}
				parts := wire.Request.Contents[1].Parts
				if len(parts) != 2 || string(parts[0].ThoughtSignature) != "sig" || parts[1].FunctionCall == nil || parts[1].FunctionCall.ID != "call-1" {
					t.Error("Signed reasoning and tool identity were not preserved")
				}
				result := wire.Request.Contents[2].Parts[0].FunctionResponse
				if result == nil || result.ID != "call-1" || result.Name != "echo" || result.Response["result"] != "OK" {
					t.Error("Tool result was not associated with its call")
				}
				_, _ = w.Write([]byte("data: " + `{"response":{"candidates":[{"content":{"parts":[{"text":"OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1,"totalTokenCount":11}}}` + "\n\n"))
			}))
			defer server.Close()
			m := &languageModel{modelID: model, options: options{apiKey: "test-token", project: "account-project", baseURL: server.URL, client: server.Client()}}
			call := accountToolCall()
			first, err := m.Generate(t.Context(), call)
			require.NoError(t, err)
			require.Equal(t, fantasy.FinishReasonToolCalls, first.FinishReason)
			call.Prompt = appendToolReply(call.Prompt, first)
			stream, err := m.Stream(t.Context(), call)
			require.NoError(t, err)
			var text string
			var finish fantasy.FinishReason
			for part := range stream {
				require.NoError(t, part.Error)
				if part.Type == fantasy.StreamPartTypeTextDelta {
					text += part.Delta
				}
				if part.Type == fantasy.StreamPartTypeFinish {
					finish = part.FinishReason
				}
			}
			require.Equal(t, "OK", text)
			require.Equal(t, fantasy.FinishReasonStop, finish)
		})
	}
}

func accountToolCall() fantasy.Call {
	return fantasy.Call{
		Prompt: fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Synthetic integration test: call echo with value OK exactly once. After receiving its result, respond with OK only."}}}},
		Tools: []fantasy.Tool{fantasy.FunctionTool{
			Name: "echo", Description: "Returns its input.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []any{"value"}},
		}},
	}
}

func appendToolReply(prompt fantasy.Prompt, response *fantasy.Response) fantasy.Prompt {
	var parts []fantasy.MessagePart
	var results []fantasy.MessagePart
	for _, content := range response.Content {
		switch content := content.(type) {
		case fantasy.ReasoningContent:
			parts = append(parts, fantasy.ReasoningPart{Text: content.Text, ProviderOptions: fantasy.ProviderOptions(content.ProviderMetadata)})
		case fantasy.TextContent:
			parts = append(parts, fantasy.TextPart{Text: content.Text})
		case fantasy.ToolCallContent:
			parts = append(parts, fantasy.ToolCallPart{ToolCallID: content.ToolCallID, ToolName: content.ToolName, Input: content.Input})
			results = append(results, fantasy.ToolResultPart{ToolCallID: content.ToolCallID, Output: fantasy.ToolResultOutputContentText{Text: "OK"}})
		}
	}
	return append(prompt, fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts}, fantasy.Message{Role: fantasy.MessageRoleTool, Content: results})
}
