package providersetup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/appenv"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/providersetup"
	"github.com/stretchr/testify/require"
)

const testKey = "sk-test-SECRET-0123456789"

// TestMain isolates the global configuration from the machine's own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "atlas-providersetup")
	if err != nil {
		panic(err)
	}
	os.Setenv(appenv.Prefix+"GLOBAL_CONFIG", dir)
	os.Setenv(appenv.Prefix+"GLOBAL_DATA", dir)
	os.Setenv(appenv.Prefix+"SUBAGENTS_DIR", filepath.Join(dir, "agents"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// newStore builds a real config store in a fresh directory, so the write
// goes through the same validation and reload path production uses.
func newStore(t *testing.T) *config.ConfigStore {
	t.Helper()
	// Distinct directories: the same file read as both layers would
	// concatenate its model list with itself.
	t.Setenv(appenv.Prefix+"GLOBAL_CONFIG", t.TempDir())
	t.Setenv(appenv.Prefix+"GLOBAL_DATA", t.TempDir())
	store, err := config.Init(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)
	return store
}

func fixedKey(key string) providersetup.KeyPrompter {
	return func(context.Context, providersetup.KeyRequest) (string, error) { return key, nil }
}

func baseParams(url string) providersetup.Params {
	return providersetup.Params{
		ID: "acme", Name: "Acme", BaseURL: url,
		DiscoverModels: true, Scope: config.ScopeGlobal,
	}
}

func openAIServer(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer "+testKey {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		var data []map[string]any
		for _, id := range ids {
			data = append(data, map[string]any{"id": id, "object": "model"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSetupOpenAIStyleListWritesProvider(t *testing.T) {
	srv := openAIServer(t, "m-one", "m-two")
	store := newStore(t)
	p := baseParams(srv.URL + "/v1")

	res, err := providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
	require.NoError(t, err)
	require.Len(t, res.Models, 2)
	require.Equal(t, srv.URL+"/v1", res.BaseURL)

	got, ok := store.Config().Providers.Get("acme")
	require.True(t, ok)
	require.Equal(t, srv.URL+"/v1", got.BaseURL)
	require.Equal(t, catwalk.TypeOpenAICompat, got.Type)
	require.Len(t, got.Models, 2)
	require.Contains(t, res.Summary(), "m-one")
	require.NotContains(t, res.Summary(), testKey)
}

func TestProbeAnthropicStyleList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != testKey || r.Header.Get("anthropic-version") == "" || r.URL.Path != "/models" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"claude-x","display_name":"Claude X","max_input_tokens":200000,"max_tokens":8192},{"id":"claude-y"}]}`)
	}))
	t.Cleanup(srv.Close)

	res, err := providersetup.Probe(t.Context(), nil, catwalk.TypeAnthropic, srv.URL, testKey)
	require.NoError(t, err)
	require.Len(t, res.Models, 2)
	require.Equal(t, "Claude X", res.Models[0].Name)
	require.EqualValues(t, 200000, res.Models[0].ContextWindow)
	require.EqualValues(t, 8192, res.Models[0].DefaultMaxTokens)
	// Nothing is invented where the response is silent.
	require.EqualValues(t, 0, res.Models[1].ContextWindow)
	require.EqualValues(t, 0, res.Models[1].DefaultMaxTokens)
}

func TestProbeGoogleStyleList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != testKey || r.URL.Path != "/v1beta/models" || r.URL.RawQuery != "" {
			http.Error(w, "bad", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, `{"models":[
		 {"name":"models/gem-a","displayName":"Gem A","inputTokenLimit":1000000,"outputTokenLimit":8192,"supportedGenerationMethods":["generateContent"]},
		 {"name":"models/embed","supportedGenerationMethods":["embedContent"]}]}`)
	}))
	t.Cleanup(srv.Close)

	res, err := providersetup.Probe(t.Context(), nil, catwalk.TypeGoogle, srv.URL, testKey)
	require.NoError(t, err)
	require.Len(t, res.Models, 1)
	require.Equal(t, "gem-a", res.Models[0].ID)
	require.EqualValues(t, 1000000, res.Models[0].ContextWindow)
}

func TestProbeBadKeyIsClearAndDoesNotEchoKey(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Upstream error bodies sometimes quote the credential.
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":"invalid key %s"}`, testKey)
		}))
		t.Cleanup(srv.Close)

		_, err := providersetup.Probe(t.Context(), nil, catwalk.TypeOpenAICompat, srv.URL, testKey)
		require.Error(t, err)
		require.Contains(t, err.Error(), "rejected the API key")
		require.NotContains(t, err.Error(), testKey)
		require.Contains(t, err.Error(), "[redacted]")
	}
}

func TestProbeFallsBackToV1AndReportsCorrectedURL(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"only"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	res, err := providersetup.Probe(t.Context(), nil, catwalk.TypeOpenAICompat, srv.URL, testKey)
	require.NoError(t, err)
	require.Equal(t, srv.URL+"/v1", res.BaseURL)
	require.Contains(t, res.Note, "corrected")
	require.EqualValues(t, 2, hits.Load())
}

func TestProbe404EverywhereNamesBothAttempts(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	_, err := providersetup.Probe(t.Context(), nil, catwalk.TypeOpenAICompat, srv.URL, testKey)
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
	require.Contains(t, err.Error(), srv.URL+"/models")
	require.Contains(t, err.Error(), srv.URL+"/v1/models")
}

func TestProbeRefusesOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[`)
		chunk := strings.Repeat(`{"id":"x"},`, 1000)
		for range 600 {
			fmt.Fprint(w, chunk)
		}
	}))
	t.Cleanup(srv.Close)

	_, err := providersetup.Probe(t.Context(), nil, catwalk.TypeOpenAICompat, srv.URL+"/v1", testKey)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to read")
}

func TestProbeRefusesTooManyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data []map[string]string
		for i := range providersetup.MaxModels + 1 {
			data = append(data, map[string]string{"id": fmt.Sprintf("m%d", i)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)

	_, err := providersetup.Probe(t.Context(), nil, catwalk.TypeOpenAICompat, srv.URL+"/v1", testKey)
	require.Error(t, err)
	require.Contains(t, err.Error(), "more than the limit")
}

func TestProbeNeverSendsTheKeyToAnotherHost(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
			leaked.Store(true)
		}
		fmt.Fprint(w, `{"data":[{"id":"evil"}]}`)
	}))
	t.Cleanup(other.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/models", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	for _, typ := range []catwalk.Type{catwalk.TypeOpenAICompat, catwalk.TypeAnthropic} {
		_, err := providersetup.Probe(t.Context(), nil, typ, srv.URL+"/v1", testKey)
		require.Error(t, err)
		require.Contains(t, err.Error(), "another host")
		require.NotContains(t, err.Error(), testKey)
	}
	require.False(t, leaked.Load(), "the key reached a host the user did not name")
}

func TestValidateInputs(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"", "Acme", "my.provider", "has space", "-lead", "openai", strings.Repeat("a", 65)} {
		require.Error(t, providersetup.ValidateID(id), "id %q", id)
	}
	for _, id := range []string{"acme", "my-provider_2", "0day"} {
		require.NoError(t, providersetup.ValidateID(id), "id %q", id)
	}

	bad := []string{
		"", "api.example.com/v1", "ftp://api.example.com", "http://api.example.com/v1",
		"https://user:pass@api.example.com/v1", "https://api.example.com/v1#frag",
		"https://api.example.com/v1?key=1", "https:///v1", "javascript:alert(1)",
	}
	for _, raw := range bad {
		_, err := providersetup.ValidateBaseURL(raw, false)
		require.Error(t, err, "url %q", raw)
	}
	for _, raw := range []string{
		"https://api.example.com/v1/", "http://localhost:11434/v1", "http://127.0.0.1:8080",
		"http://[::1]:8080/v1", "http://dev.localhost/v1",
	} {
		u, err := providersetup.ValidateBaseURL(raw, false)
		require.NoError(t, err, "url %q", raw)
		require.False(t, strings.HasSuffix(u.String(), "/"), "trailing slash kept on %q", raw)
	}
	_, err := providersetup.ValidateBaseURL("http://api.example.com/v1", true)
	require.NoError(t, err, "allow_insecure opts in to plain http")
	_, err = providersetup.ValidateBaseURL("http://api.example.com/v1", false)
	require.ErrorContains(t, err, "unencrypted")

	_, err = providersetup.ParseType("bedrock")
	require.Error(t, err)
	typ, err := providersetup.ParseType("")
	require.NoError(t, err)
	require.Equal(t, catwalk.TypeOpenAICompat, typ)
	require.Error(t, providersetup.ValidateEnvName("sk-abc123"), "a value is not an env var name")
	require.NoError(t, providersetup.ValidateEnvName("$ACME_KEY"))
}

func TestSetupRejectsDuplicateUnlessOverwrite(t *testing.T) {
	srv := openAIServer(t, "m1")
	store := newStore(t)
	p := baseParams(srv.URL + "/v1")

	_, err := providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
	require.NoError(t, err)

	_, err = providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
	require.ErrorContains(t, err, "already exists")

	p.Overwrite = true
	res, err := providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
	require.NoError(t, err)
	require.True(t, res.Replaced)
}

func TestSetupAcceptsLocalhostHTTPAndRejectsRemoteHTTP(t *testing.T) {
	store := newStore(t)
	p := baseParams("http://api.example.com/v1")
	_, err := providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
	require.ErrorContains(t, err, "plain http")

	srv := openAIServer(t, "m1") // 127.0.0.1 over http.
	_, err = providersetup.Setup(t.Context(), store, baseParams(srv.URL+"/v1"), fixedKey(testKey))
	require.NoError(t, err)
}

func TestSetupWithoutPromptOrEnvFailsClearlyAndWritesNothing(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	store := newStore(t)

	p := baseParams(srv.URL + "/v1")
	_, err := providersetup.Setup(t.Context(), store, p, nil)
	require.ErrorContains(t, err, "api_key_env")

	p.APIKeyEnv = "ATLAS_TEST_UNSET_KEY_VAR"
	_, err = providersetup.Setup(t.Context(), store, p, nil)
	require.ErrorContains(t, err, "ATLAS_TEST_UNSET_KEY_VAR is not set")

	require.Zero(t, hits.Load(), "no request is made without a key")
	_, ok := store.Config().Providers.Get("acme")
	require.False(t, ok)
}

func TestSetupUsesEnvVarAndStoresOnlyItsName(t *testing.T) {
	srv := openAIServer(t, "m1")
	t.Setenv("ATLAS_TEST_ACME_KEY", testKey)
	store := newStore(t)
	p := baseParams(srv.URL + "/v1")
	p.APIKeyEnv = "ATLAS_TEST_ACME_KEY"

	res, err := providersetup.Setup(t.Context(), store, p, nil)
	require.NoError(t, err)
	require.Contains(t, res.KeyLocation, "ATLAS_TEST_ACME_KEY")

	path, err := store.ConfigFilePath(config.ScopeGlobal)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), testKey)
	require.Contains(t, string(raw), `"$ATLAS_TEST_ACME_KEY"`)
}

func TestSetupFailedProbeWritesNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key "+testKey, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	store := newStore(t)
	path, err := store.ConfigFilePath(config.ScopeGlobal)
	require.NoError(t, err)

	_, err = providersetup.Setup(t.Context(), store, baseParams(srv.URL+"/v1"), fixedKey(testKey))
	require.ErrorContains(t, err, "nothing was written")
	require.NotContains(t, err.Error(), testKey)

	_, ok := store.Config().Providers.Get("acme")
	require.False(t, ok)
	if raw, rerr := os.ReadFile(path); rerr == nil {
		require.NotContains(t, string(raw), "acme")
		require.NotContains(t, string(raw), testKey)
	}
}

func TestSetupWithoutDiscoveryNeedsModelsAndMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	store := newStore(t)

	p := baseParams(srv.URL + "/v1")
	p.DiscoverModels = false
	_, err := providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
	require.ErrorContains(t, err, "at least one model")

	p.Models = []string{"custom-1"}
	res, err := providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
	require.NoError(t, err)
	require.Zero(t, hits.Load())
	require.Len(t, res.Models, 1)
	require.NotEmpty(t, res.Notes, "skipping the check is said out loud")
	require.Contains(t, strings.Join(res.Notes, " "), "not checked")
}

func TestSetupWorkspaceScopeRefusesToSaveAKeyInTheProject(t *testing.T) {
	srv := openAIServer(t, "m1")
	store := newStore(t)
	p := baseParams(srv.URL + "/v1")
	p.Scope = config.ScopeWorkspace

	_, err := providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
	require.ErrorContains(t, err, "api_key_env")
}

// capture routes slog into a buffer for the duration of the test.
func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// The key must not appear in anything Setup returns or logs, on the
// success path or on any failure path, even when the server echoes it.
func TestKeyNeverAppearsInOutputsErrorsOrLogs(t *testing.T) {
	logs := capture(t)
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"fine","display_name":"plain"}]}`)
		case "/html/v1/models":
			fmt.Fprintf(w, "<html>key %s was bad</html>", testKey)
		default:
			http.Error(w, "echo "+testKey, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(echo.Close)

	var all []string
	store := newStore(t)
	for _, path := range []string{"/ok/v1", "/html/v1", "/boom/v1"} {
		p := baseParams(echo.URL + path)
		p.Overwrite = true
		res, err := providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
		if err != nil {
			all = append(all, err.Error())
		} else {
			all = append(all, res.Summary())
		}
	}
	// A key with whitespace is rejected; the message must not quote it.
	_, err := providersetup.Setup(t.Context(), store, baseParams(echo.URL+"/ok/v1"), fixedKey("two words"))
	require.Error(t, err)
	all = append(all, err.Error())

	require.Len(t, all, 4)
	for _, s := range all {
		require.NotContains(t, s, testKey)
	}
	require.NotContains(t, logs.String(), testKey)
	require.NotContains(t, logs.String(), "two words")
}
