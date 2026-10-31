package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/appenv"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/secretprompt"
	"github.com/stretchr/testify/require"
)

const providerToolKey = "sk-tool-SECRET-9876543210"

// providerRecordingPerms grants everything and remembers what was asked.
type providerRecordingPerms struct {
	permission.Service
	mu   sync.Mutex
	reqs []permission.CreatePermissionRequest
}

func (r *providerRecordingPerms) Request(_ context.Context, req permission.CreatePermissionRequest) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req)
	return true, nil
}

func providerTestStore(t *testing.T) *config.ConfigStore {
	t.Helper()
	t.Setenv(appenv.Prefix+"GLOBAL_CONFIG", t.TempDir())
	t.Setenv(appenv.Prefix+"GLOBAL_DATA", t.TempDir())
	t.Setenv(appenv.Prefix+"SUBAGENTS_DIR", t.TempDir())
	store, err := config.Init(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)
	return store
}

func modelServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+providerToolKey {
			http.Error(w, "bad key "+providerToolKey, http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"alpha"},{"id":"beta"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runAddProvider(t *testing.T, tool fantasy.AgentTool, input map[string]any) fantasy.ToolResponse {
	t.Helper()
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "sess-1")
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "call-1", Name: AtlasConfigToolName, Input: string(raw)})
	require.NoError(t, err)
	return resp
}

// answerSecrets plays the user: it types key into every key prompt.
func answerSecrets(t *testing.T, svc secretprompt.Service, key string) (prompts func() []secretprompt.Request) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	events := svc.Subscribe(ctx)
	var mu sync.Mutex
	var seen []secretprompt.Request
	go func() {
		for ev := range events {
			mu.Lock()
			seen = append(seen, ev.Payload)
			mu.Unlock()
			svc.Submit(ev.Payload.ID, key)
		}
	}()
	return func() []secretprompt.Request {
		mu.Lock()
		defer mu.Unlock()
		return append([]secretprompt.Request(nil), seen...)
	}
}

// The key is typed into a masked prompt; it must not show up in the tool
// result, the permission prompt, the prompt request published to the
// interface, or the logs.
func TestAddProviderKeyNeverReachesTheModelOrTheLogs(t *testing.T) {
	var logs strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv := modelServer(t)
	store := providerTestStore(t)
	perms := &providerRecordingPerms{}
	secrets := secretprompt.NewService()
	secrets.SetAvailable(true)
	prompts := answerSecrets(t, secrets, providerToolKey)

	tool := NewAtlasConfigTool(perms, store, store.WorkingDir(), secrets)
	resp := runAddProvider(t, tool, map[string]any{
		"action": "add_provider", "id": "acme", "base_url": srv.URL + "/v1",
	})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "alpha")
	require.Contains(t, resp.Content, "acme")

	require.Len(t, prompts(), 1, "the user was asked once, out of band")
	require.Len(t, perms.reqs, 1)
	require.Contains(t, perms.reqs[0].Description, "Your API key will be sent to "+strings.TrimPrefix(srv.URL, "http://"))

	haystacks := []string{resp.Content, resp.Metadata, logs.String()}
	for _, req := range perms.reqs {
		haystacks = append(haystacks, req.Description)
		blob, err := json.Marshal(req.Params)
		require.NoError(t, err)
		haystacks = append(haystacks, string(blob))
	}
	for _, p := range prompts() {
		blob, err := json.Marshal(p)
		require.NoError(t, err)
		haystacks = append(haystacks, string(blob))
	}
	for i, h := range haystacks {
		require.NotContains(t, h, providerToolKey, "haystack %d", i)
	}
}

func TestAddProviderBadKeyIsReportedWithoutEchoingIt(t *testing.T) {
	srv := modelServer(t)
	store := providerTestStore(t)
	secrets := secretprompt.NewService()
	secrets.SetAvailable(true)
	answerSecrets(t, secrets, "sk-wrong-KEY-000000")

	tool := NewAtlasConfigTool(&providerRecordingPerms{}, store, store.WorkingDir(), secrets)
	resp := runAddProvider(t, tool, map[string]any{"action": "add_provider", "id": "acme", "base_url": srv.URL + "/v1"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "rejected the API key")
	require.NotContains(t, resp.Content, "sk-wrong-KEY-000000")
	_, ok := store.Config().Providers.Get("acme")
	require.False(t, ok, "nothing is written when the probe fails")
}

func TestAddProviderWithoutAnInterfaceNeedsAnEnvVar(t *testing.T) {
	srv := modelServer(t)
	store := providerTestStore(t)

	// No secret service at all, and one that has no UI attached.
	for _, tool := range []fantasy.AgentTool{
		NewAtlasConfigTool(&providerRecordingPerms{}, store, store.WorkingDir()),
		NewAtlasConfigTool(&providerRecordingPerms{}, store, store.WorkingDir(), secretprompt.NewService()),
	} {
		resp := runAddProvider(t, tool, map[string]any{"action": "add_provider", "id": "acme", "base_url": srv.URL + "/v1"})
		require.True(t, resp.IsError)
		require.Contains(t, resp.Content, "api_key_env")
	}

	t.Setenv("ATLAS_TOOL_TEST_KEY", providerToolKey)
	tool := NewAtlasConfigTool(&providerRecordingPerms{}, store, store.WorkingDir())
	resp := runAddProvider(t, tool, map[string]any{
		"action": "add_provider", "id": "acme", "base_url": srv.URL + "/v1", "api_key_env": "ATLAS_TOOL_TEST_KEY",
	})
	require.False(t, resp.IsError, resp.Content)
	require.NotContains(t, resp.Content, providerToolKey)
	require.Contains(t, resp.Content, "ATLAS_TOOL_TEST_KEY")
}

func TestAddProviderInvalidInputIsRefusedBeforeAnyPrompt(t *testing.T) {
	store := providerTestStore(t)
	perms := &providerRecordingPerms{}
	tool := NewAtlasConfigTool(perms, store, store.WorkingDir())

	for name, in := range map[string]map[string]any{
		"dotted id":  {"id": "my.provider", "base_url": "https://api.example.com/v1"},
		"upper id":   {"id": "Acme", "base_url": "https://api.example.com/v1"},
		"plain http": {"id": "acme", "base_url": "http://api.example.com/v1"},
		"no url":     {"id": "acme"},
		"key as env": {"id": "acme", "base_url": "https://api.example.com/v1", "api_key_env": "sk-abc-123"},
	} {
		in["action"] = "add_provider"
		resp := runAddProvider(t, tool, in)
		require.True(t, resp.IsError, name)
	}
	require.Empty(t, perms.reqs, "invalid requests never reach the permission prompt")
}

func TestAddProviderIsListedAndNotReadOnly(t *testing.T) {
	require.Contains(t, atlasConfigActions, "add_provider")
	require.False(t, atlasConfigReadOnly["add_provider"])
	require.Contains(t, atlasConfigDescription, "add_provider")
}
