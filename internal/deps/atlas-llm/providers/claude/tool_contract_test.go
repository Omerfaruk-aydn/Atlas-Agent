package claude

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
	"github.com/stretchr/testify/require"
)

type contractArguments struct {
	Action     string `json:"action"`
	Automation struct {
		WindowID string `json:"window_id"`
		Name     string `json:"name,omitempty"`
	} `json:"automation,omitempty"`
	Desktop struct {
		Mode string `json:"mode"`
	} `json:"desktop,omitempty"`
	Argv []string `json:"argv,omitempty"`
	X    int      `json:"x,omitempty"`
	Text string   `json:"text,omitempty"`
}

func contractTool() fantasy.FunctionTool {
	return fantasy.FunctionTool{Name: "computer", InputSchema: schema.ToMap(schema.Generate(reflect.TypeFor[contractArguments]()))}
}

func TestClaudeToolContractRestoresWireNamesAndTypedArguments(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			var request map[string]any
			input := `{"action":"click","automation":"{\"window_id\":\"11\"}","x":"1172","text":"{\"untouched\":true}"}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				if !streaming {
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(w, `{"id":"m1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"c1","name":"_computer","input":%s}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`, input)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-5\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
				_, _ = fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"c1\",\"name\":\"_computer\",\"input\":{}}}\n\n")
				partial, _ := json.Marshal(input)
				_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", partial)
				_, _ = fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			}))
			defer srv.Close()
			provider, err := New(WithAccessToken("fixture"), WithBaseURL(srv.URL))
			require.NoError(t, err)
			model, err := provider.LanguageModel(t.Context(), "claude-sonnet-5")
			require.NoError(t, err)
			choice := fantasy.SpecificToolChoice("computer")
			call := fantasy.Call{Prompt: []fantasy.Message{fantasy.NewUserMessage("test")}, Tools: []fantasy.Tool{contractTool()}, ToolChoice: &choice}
			var name, actual string
			if streaming {
				stream, streamErr := model.Stream(t.Context(), call)
				require.NoError(t, streamErr)
				for part := range stream {
					require.NoError(t, part.Error)
					if part.Type == fantasy.StreamPartTypeToolInputStart {
						require.Equal(t, "computer", part.ToolCallName)
					}
					if part.Type == fantasy.StreamPartTypeToolCall {
						name, actual = part.ToolCallName, part.ToolCallInput
					}
				}
			} else {
				response, responseErr := model.Generate(t.Context(), call)
				require.NoError(t, responseErr)
				c := response.Content[0].(fantasy.ToolCallContent)
				name, actual = c.ToolName, c.Input
			}
			require.Equal(t, "computer", name)
			var parsed contractArguments
			require.NoError(t, json.Unmarshal([]byte(actual), &parsed))
			require.Equal(t, "11", parsed.Automation.WindowID)
			require.Equal(t, 1172, parsed.X)
			require.Equal(t, `{"untouched":true}`, parsed.Text)
			require.Equal(t, "_computer", request["tool_choice"].(map[string]any)["name"])
			require.Equal(t, fantasy.SpecificToolChoice("computer"), *call.ToolChoice)
		})
	}
}
