package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// The MiniMax subscription providers send the OAuth access token the way
// mmx does for chat: in the x-api-key header, without the token in an Authorization
// header, unlike the API-key MiniMax providers.
func TestMiniMaxPlanSendsAccessTokenAsXAPIKey(t *testing.T) {
	for _, id := range []string{"minimax-plan", "minimax-plan-cn"} {
		var got http.Header
		var path string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, path = r.Header.Clone(), r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"MiniMax-M3","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
		}))

		c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(t.TempDir())}
		prov, err := c.buildAnthropicProvider(srv.URL, "oauth-access-token", nil, id)
		require.NoError(t, err, id)
		lm, err := prov.LanguageModel(t.Context(), "MiniMax-M3")
		require.NoError(t, err, id)
		maxTokens := int64(16)
		_, err = lm.Generate(t.Context(), fantasy.Call{
			Prompt:          fantasy.Prompt{fantasy.NewUserMessage("hello")},
			MaxOutputTokens: &maxTokens,
		})
		srv.Close()
		require.NoError(t, err, id)
		require.Equal(t, "/v1/messages", path, id)
		require.Equal(t, "oauth-access-token", got.Get("X-Api-Key"), id)
		require.NotContains(t, got.Get("Authorization"), "oauth-access-token", id)
	}
}
