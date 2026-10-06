package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeHeadersPreserveConfiguredSession(t *testing.T) {
	t.Parallel()
	headers := openCodeHeaders(map[string]string{"X-OpenCode-Session": "configured-session", "user-agent": "generic-sdk", "X-Proxy": "preserved"})
	require.Equal(t, "configured-session", headers["x-opencode-session"])
	require.Equal(t, userAgent, headers["User-Agent"])
	require.Equal(t, "preserved", headers["X-Proxy"])
	require.NotContains(t, headers, "X-OpenCode-Session")
	require.NotContains(t, headers, "user-agent")
	require.NotEqual(t, openCodeHeaders(map[string]string{})["x-opencode-session"], openCodeHeaders(map[string]string{})["x-opencode-session"])
}

func TestOpenCodeFreeTierServerRejection(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"FreeTierError","message":"OpenCode's free tier can only be used from within OpenCode"}}`))
	}))
	defer server.Close()
	p, err := coord.buildProvider(config.ProviderConfig{ID: "opencode-zen", Type: catwalk.TypeOpenAICompat, APIKey: "test-key", BaseURL: server.URL + "/zen/v1"}, config.SelectedModel{Model: "mimo-v2.6-flash-free"}, false)
	require.NoError(t, err)
	lm, err := p.LanguageModel(t.Context(), "mimo-v2.6-flash-free")
	require.NoError(t, err)
	_, err = lm.Generate(t.Context(), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("hello")}})
	var providerErr *fantasy.ProviderError
	require.ErrorAs(t, err, &providerErr)
	require.Equal(t, http.StatusForbidden, providerErr.StatusCode)
	require.False(t, providerErr.IsRetryable())
	// The local server is intentionally not classified as opencode.ai; the
	// provider preserves upstream evidence instead of changing its identity.
	providerErr.URL = "https://opencode.ai/zen/v1/chat/completions"
	require.Contains(t, providerErr.Error(), "server restricts")
}
