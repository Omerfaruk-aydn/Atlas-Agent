package antigravity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestResolveAccountModelRoutes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ model, effort, want string }{
		{"gemini-4-argon", "high", "gemini-4-argon-high"},
		{"gemini-4-argon-low", "", "gemini-4-argon-low"},
		{"gemini-3.8-flash", "medium", "gemini-3.8-flash-medium"},
		{"gemini-3.7-flash", "low", "gemini-3.7-flash-low"},
		{"gemini-3.6-flash", "high", "gemini-3.6-flash-high"},
		{"gemini-3.8-flash", "", "gemini-3.8-flash-medium"},
		{"gemini-3.1-pro", "", "gemini-3.1-pro-high"},
	} {
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()
			m := &languageModel{modelID: test.model, options: options{project: "account-project"}}
			env, _, err := m.buildEnvelope(fantasy.Call{ProviderOptions: fantasy.ProviderOptions{Name: &ProviderOptions{ThinkingLevel: test.effort}}})
			require.NoError(t, err)
			require.Equal(t, test.want, env.Model)
			require.Equal(t, "agent", env.RequestType)
			require.True(t, strings.HasPrefix(env.RequestID, "agent-"))
			if env.Request.GenerationConfig != nil {
				require.Nil(t, env.Request.GenerationConfig.ThinkingConfig)
			}
		})
	}
	_, _, err := resolveModel("gemini-3.1-pro", "medium")
	require.Error(t, err)
}

func TestWrappedResponsesWorkForGenerateAndStream(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("Unexpected generation route: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var wire envelope
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
			return
		}
		if wire.Model != "gemini-3.8-flash-medium" {
			t.Errorf("Incorrect model route: %s", wire.Model)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"OK\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1,\"totalTokenCount\":3}}}\n\n"))
	}))
	defer server.Close()
	m := &languageModel{modelID: "gemini-3.8-flash", options: options{project: "account-project", apiKey: "test-token", baseURL: server.URL, client: server.Client()}}
	response, err := m.Generate(t.Context(), fantasy.Call{})
	require.NoError(t, err)
	require.Equal(t, int64(3), response.Usage.TotalTokens)
	require.Equal(t, "OK", response.Content[0].(fantasy.TextContent).Text)
	stream, err := m.Stream(t.Context(), fantasy.Call{})
	require.NoError(t, err)
	var text string
	for part := range stream {
		if part.Type == fantasy.StreamPartTypeTextDelta {
			text += part.Delta
		}
		require.NoError(t, part.Error)
	}
	require.Equal(t, "OK", text)
}
