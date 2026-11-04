package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestConnectedAccountToolPerformsOperation(t *testing.T) {
	called := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		require.Equal(t, "/chat.postMessage", r.URL.Path)
		_, _ = w.Write([]byte(`{"ok":true,"ts":"123"}`))
	}))
	defer provider.Close()
	svc := connections.New(config.NewTestStore(&config.Config{}), t.TempDir())
	svc.ProviderAPIURLs = map[string]string{"slack": provider.URL}
	require.NoError(t, svc.Save("slack", connections.SaveRequest{Token: "token"}))
	tool := voiceTool(t, NewConnectedAccountTools(svc), "slack")
	response, err := tool.Run(context.Background(), fantasy.ToolCall{ID: "call-1", Name: "slack", Input: `{"action":"send_message","channel":"C123","text":"Hello"}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Contains(t, response.Content, "provider accepted")
	require.True(t, called)
}
