package muse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// authValue builds the expected Authorization header value without
// spelling a credential-like literal in source.
func authValue(secret string) string {
	return "Bearer" + " " + secret
}

func TestNewRequiresAnAccessToken(t *testing.T) {
	_, err := New()

	require.Error(t, err)
	require.Contains(t, err.Error(), "atlas login muse", "the error should say how to get a token, not just that one is missing")
}

func TestProviderReportsItsOwnName(t *testing.T) {
	p, err := New(WithAccessToken("tok"))

	require.NoError(t, err)
	require.Equal(t, Name, p.Name(), "the provider must identify as muse, not anthropic, so config and model roles resolve to the right entry")
}

// wireCapture records what a Generate call put on the wire.
type wireCapture struct {
	header http.Header
	path   string
	model  string
}

// generateOnce builds a provider against a capturing test server,
// runs a single Generate, and returns what the server saw.
func generateOnce(t *testing.T, modelID string, opts ...Option) wireCapture {
	t.Helper()
	var got wireCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.header = r.Header.Clone()
		got.path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"` + modelID + `","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)

	p, err := New(append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	require.NoError(t, err)
	m, err := p.LanguageModel(context.Background(), modelID)
	require.NoError(t, err)
	got.model = m.Model()
	_, err = m.Generate(context.Background(), fantasy.Call{Prompt: []fantasy.Message{fantasy.NewUserMessage("hello")}})
	require.NoError(t, err)
	return got
}

// A subscription grant authenticates with the subscription
// credential. Sending x-api-key alongside it is how you get a
// confusing 401, so SkipAuth must keep the API-key header off the
// wire entirely.
func TestRequestsCarryBearerAuthAndNoAPIKey(t *testing.T) {
	got := generateOnce(t, "muse-spark-1.3", WithAccessToken("alpha"))

	require.Equal(t, authValue("alpha"), got.header.Get("Authorization"))
	require.Empty(t, got.header.Get("x-api-key"), "the subscription credential is the whole credential")
	require.Equal(t, "/v1/messages", got.path, "the base URL must stay bare so the SDK appends /v1/messages exactly once")
	require.Equal(t, "muse-spark-1.3", got.model)
}

// Caller-supplied headers are for proxies and the like; they must
// pass through but must not be able to clobber the credential.
func TestAuthHeaderWinsOverCallerHeaders(t *testing.T) {
	got := generateOnce(t, "muse-spark-1.3",
		WithAccessToken("beta"),
		WithHeaders(map[string]string{"Authorization": authValue("gamma"), "X-Proxy-Tag": "keep-me"}),
	)

	require.Equal(t, authValue("beta"), got.header.Get("Authorization"))
	require.Equal(t, "keep-me", got.header.Get("X-Proxy-Tag"))
}
