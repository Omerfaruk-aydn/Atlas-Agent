package muse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func jsonDepth(v any) int {
	depth := 0
	switch v := v.(type) {
	case map[string]any:
		for _, child := range v {
			depth = max(depth, jsonDepth(child))
		}
		return depth + 1
	case []any:
		for _, child := range v {
			depth = max(depth, jsonDepth(child))
		}
		return depth + 1
	}
	return 0
}

func TestMuseDeepToolSchemaFitsWireLimitWithoutMutatingOriginal(t *testing.T) {
	t.Parallel()
	leaf := map[string]any{"type": "string", "enum": []any{"allowed"}}
	var schema map[string]any = leaf
	for range 14 {
		schema = map[string]any{"type": "object", "properties": map[string]any{"child": schema}, "required": []any{"child"}}
	}
	original, err := json.Marshal(schema)
	require.NoError(t, err)
	requests := make(chan map[string]any, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad JSON", 400)
			return
		}
		requests <- body
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"muse-spark-1.3\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":0}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"muse-spark-1.3","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	p, err := New(WithAccessToken("test"), WithBaseURL(srv.URL))
	require.NoError(t, err)
	m, err := p.LanguageModel(t.Context(), "muse-spark-1.3")
	require.NoError(t, err)
	_, err = m.Generate(context.Background(), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("hello")}, Tools: []fantasy.Tool{fantasy.FunctionTool{Name: "deep", InputSchema: schema}}})
	require.NoError(t, err)
	body := <-requests
	wire := body["tools"].([]any)[0].(map[string]any)["input_schema"]
	require.LessOrEqual(t, jsonDepth(wire), 10)
	after, err := json.Marshal(schema)
	require.NoError(t, err)
	require.Equal(t, string(original), string(after))
	wireJSON, _ := json.Marshal(wire)
	require.Contains(t, string(wireJSON), "allowed", "deep constraints must remain available as guidance")
	stream, err := m.Stream(t.Context(), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("hello")}, Tools: []fantasy.Tool{fantasy.FunctionTool{Name: "deep", InputSchema: schema}}})
	require.NoError(t, err)
	for part := range stream {
		require.NotEqual(t, fantasy.StreamPartTypeError, part.Type)
	}
	body = <-requests
	wire = body["tools"].([]any)[0].(map[string]any)["input_schema"]
	require.LessOrEqual(t, jsonDepth(wire), 10)
}
