package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools/mcp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/stretchr/testify/require"
)

const testKey = "sk-test-0123456789abcdefSECRET"

// fakeBackend is an in-memory ConfigBackend. Writes go to the config and
// to calls; setErr makes every write fail.
type fakeBackend struct {
	mu         sync.Mutex
	cfg        *config.Config
	known      []catwalk.Provider
	paths      []string
	setErr     error
	refreshErr error
	calls      []string
}

func (b *fakeBackend) Config() *config.Config {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg
}
func (b *fakeBackend) Resolver() config.VariableResolver  { return nil }
func (b *fakeBackend) KnownProviders() []catwalk.Provider { return b.known }
func (b *fakeBackend) LoadedPaths() []string              { return b.paths }

func (b *fakeBackend) SetConfigField(_ config.Scope, key string, value any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.setErr != nil {
		return b.setErr
	}
	b.calls = append(b.calls, "set "+key)
	if id, ok := strings.CutSuffix(strings.TrimPrefix(key, "providers."), ".disable"); ok {
		pc, _ := b.cfg.Providers.Get(id)
		pc.Disable = value.(bool)
		b.cfg.Providers.Set(id, pc)
	}
	return nil
}

func (b *fakeBackend) RemoveConfigField(_ config.Scope, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.setErr != nil {
		return b.setErr
	}
	b.calls = append(b.calls, "remove "+key)
	return nil
}

func (b *fakeBackend) RefreshOAuthToken(context.Context, config.Scope, string) error {
	return b.refreshErr
}

func (b *fakeBackend) setProvider(id string, pc config.ProviderConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cfg.Providers.Set(id, pc)
}

// newBackend writes a workspace atlas.json defining the given provider and
// MCP keys so they are editable, and returns a backend whose config holds
// them.
func newBackend(t *testing.T, providers map[string]config.ProviderConfig, mcps config.MCPs) *fakeBackend {
	t.Helper()
	dir := t.TempDir()
	ws := filepath.Join(dir, "atlas.json")
	doc := map[string]any{"providers": map[string]any{}, "mcp": map[string]any{}}
	for id := range providers {
		doc["providers"].(map[string]any)[id] = map[string]any{"api_key": "x"}
	}
	for name := range mcps {
		doc["mcp"].(map[string]any)[name] = map[string]any{"type": "stdio"}
	}
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(ws, data, 0o600))
	return &fakeBackend{
		cfg: &config.Config{
			Options:   &config.Options{DataDirectory: dir},
			Providers: csync.NewMapFrom(providers),
			MCP:       mcps,
		},
		paths: []string{ws},
	}
}

// fakeAdapter serves fixed items with a scripted check.
type fakeAdapter struct {
	mu    sync.Mutex
	items []Item
	parts map[string]any
	check func(ctx context.Context, item Item) CheckResult
}

func (a *fakeAdapter) Items(_ context.Context, fp Fingerprinter) []Item {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Item, len(a.items))
	for i, it := range a.items {
		it.fingerprint = fp.Fingerprint(it.ID, a.parts[it.ID])
		out[i] = it
	}
	return out
}

func (a *fakeAdapter) Check(ctx context.Context, item Item) CheckResult {
	return a.check(ctx, item)
}

func (a *fakeAdapter) Do(context.Context, Item, Action) (i18n.Message, error) {
	return i18n.Message{}, ErrUnsupported
}

func (a *fakeAdapter) setPart(id string, v any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.parts[id] = v
}

func tool(id string) Item {
	return Item{ID: id, Name: id, Kind: KindTool, Status: StatusUnchecked, Actions: []Action{ActionTest}}
}

func TestInventoryFiltersAndCatalog(t *testing.T) {
	t.Parallel()
	b := newBackend(t, map[string]config.ProviderConfig{
		"openai": {ID: "openai", Name: "OpenAI", APIKey: testKey, Models: []catwalk.Model{{ID: "a", CanReason: true}}},
	}, nil)
	b.known = []catwalk.Provider{{ID: "openai", Name: "OpenAI"}, {ID: "groq", Name: "Groq", APIKey: "$GROQ_API_KEY"}}
	s := New(Options{})
	s.Add(&ProviderAdapter{Backend: b, Secrets: s.Secrets()})
	s.Add(&fakeAdapter{items: []Item{tool("tool:git")}, parts: map[string]any{}})

	all := s.Snapshot(t.Context(), Filter{})
	require.Len(t, all, 3)
	require.Equal(t, KindProvider, all[0].Kind)

	configured := s.Snapshot(t.Context(), Filter{Configured: true})
	require.Len(t, configured, 2)

	groq, ok := s.Get(t.Context(), "provider:groq")
	require.True(t, ok)
	require.Equal(t, StatusNotConfigured, groq.Status)
	require.Equal(t, "Set GROQ_API_KEY or add a key from /model.", groq.Remedy.String())
	require.Equal(t, []Action{ActionConfigure}, groq.Actions)

	openai, ok := s.Get(t.Context(), "provider:openai")
	require.True(t, ok)
	require.Equal(t, StatusUnchecked, openai.Status, "configured is not usable until checked")
	require.True(t, openai.Source.Editable)
	require.Contains(t, openai.Actions, ActionDisable)
	require.Contains(t, openai.Actions, ActionForgetCredentials)

	require.Len(t, s.Snapshot(t.Context(), Filter{Query: "reasoning"}), 1)
	require.Len(t, s.Snapshot(t.Context(), Filter{Kind: KindTool}), 1)
	require.Empty(t, s.Snapshot(t.Context(), Filter{Status: "attention"}))
}

func TestNonEditableSourceOffersNoWrites(t *testing.T) {
	t.Parallel()
	b := newBackend(t, map[string]config.ProviderConfig{"openai": {ID: "openai", APIKey: testKey}}, nil)
	// A shell config loaded after the JSON file overrides it, so Atlas
	// must not edit the JSON file.
	b.paths = append(b.paths, filepath.Join(t.TempDir(), "atlasrc"))
	s := New(Options{})
	s.Add(&ProviderAdapter{Backend: b, Secrets: s.Secrets()})

	it, ok := s.Get(t.Context(), "provider:openai")
	require.True(t, ok)
	require.False(t, it.Source.Editable)
	require.NotContains(t, it.Actions, ActionDisable)
	require.Contains(t, it.Remedy.String(), "edit that file")

	_, err := s.Do(t.Context(), "provider:openai", ActionDisable)
	require.ErrorIs(t, err, ErrUnsupported)
	require.Empty(t, b.calls)
}

func TestConfigSaveFailureIsNotSuccess(t *testing.T) {
	t.Parallel()
	b := newBackend(t, map[string]config.ProviderConfig{"openai": {ID: "openai", APIKey: testKey}}, nil)
	b.setErr = errors.New("disk full while writing " + testKey)
	s := New(Options{})
	s.Add(&ProviderAdapter{Backend: b, Secrets: s.Secrets()})
	_, _ = s.Get(t.Context(), "provider:openai") // Registers the key.

	msg, err := s.Do(t.Context(), "provider:openai", ActionDisable)
	require.Error(t, err)
	require.True(t, msg.IsZero())
	require.NotContains(t, err.Error(), testKey)
	pc, _ := b.cfg.Providers.Get("openai")
	require.False(t, pc.Disable)

	b.setErr = nil
	msg, err = s.Do(t.Context(), "provider:openai", ActionDisable)
	require.NoError(t, err)
	require.Contains(t, msg.String(), "Credentials were kept")
	it, _ := s.Get(t.Context(), "provider:openai")
	require.Equal(t, StatusDisabled, it.Status)
	require.Contains(t, it.Actions, ActionEnable)
}

func TestProviderCheckResultsAndSecretRedaction(t *testing.T) {
	t.Parallel()
	b := newBackend(t, map[string]config.ProviderConfig{"openai": {ID: "openai", APIKey: testKey}}, nil)
	statePath := filepath.Join(t.TempDir(), "checks.json")
	var calls atomic.Int32
	var fail atomic.Bool
	s := New(Options{StatePath: statePath})
	s.Add(&ProviderAdapter{Backend: b, Secrets: s.Secrets(), Test: func(context.Context, config.ProviderConfig, config.VariableResolver) error {
		calls.Add(1)
		if fail.Load() {
			return errors.New("401 Unauthorized: invalid api key " + testKey)
		}
		return nil
	}})

	it, err := s.Check(t.Context(), "provider:openai")
	require.NoError(t, err)
	require.Equal(t, StatusReady, it.Status)
	require.False(t, it.CheckedAt.IsZero())

	fail.Store(true)
	it, err = s.Check(t.Context(), "provider:openai")
	require.NoError(t, err)
	require.Equal(t, StatusNeedsLogin, it.Status)
	require.NotContains(t, it.LastError.String(), testKey)
	require.Contains(t, it.LastError.String(), "401")
	require.Equal(t, int32(2), calls.Load())

	snap, err := json.Marshal(s.Snapshot(t.Context(), Filter{}))
	require.NoError(t, err)
	require.NotContains(t, string(snap), testKey)
	stored, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.NotContains(t, string(stored), testKey)
}

func TestUnverifiableProviderIsLimited(t *testing.T) {
	t.Parallel()
	b := newBackend(t, map[string]config.ProviderConfig{"bedrock": {ID: "bedrock", Type: catwalk.TypeBedrock, APIKey: testKey}}, nil)
	s := New(Options{})
	s.Add(&ProviderAdapter{Backend: b, Secrets: s.Secrets(), Test: func(context.Context, config.ProviderConfig, config.VariableResolver) error { return nil }})
	it, err := s.Check(t.Context(), "provider:bedrock")
	require.NoError(t, err)
	require.Equal(t, StatusLimited, it.Status)
	require.Contains(t, it.CheckNote.String(), "billed request")
}

func TestOAuthRefreshFailure(t *testing.T) {
	t.Parallel()
	refresh := "rt-refresh-secret-value-123456"
	b := newBackend(t, map[string]config.ProviderConfig{"claude": {ID: "claude", OAuthToken: &oauth.Token{
		AccessToken: "at-access-secret-value-123456", RefreshToken: refresh, ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	}}}, nil)
	b.refreshErr = errors.New("token exchange failed: refresh_token=" + refresh + " revoked")
	s := New(Options{})
	s.Add(&ProviderAdapter{Backend: b, Secrets: s.Secrets()})

	it, err := s.Check(t.Context(), "account:claude")
	require.NoError(t, err)
	require.Equal(t, KindAccount, it.Kind)
	require.Equal(t, StatusNeedsLogin, it.Status)
	require.NotContains(t, it.LastError.String(), refresh)
	require.Equal(t, "Sign in again from /model.", it.Remedy.String())

	// A valid token is checked locally.
	b.setProvider("claude", config.ProviderConfig{ID: "claude", OAuthToken: &oauth.Token{AccessToken: "at-access-secret-value-123456", ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}})
	it, err = s.Check(t.Context(), "account:claude")
	require.NoError(t, err)
	require.Equal(t, StatusReady, it.Status)
}

type fakeMCP struct {
	states map[string]mcp.ClientInfo
	probe  func(ctx context.Context, name string) (mcp.ProbeResult, error)
}

func (f *fakeMCP) States() map[string]mcp.ClientInfo { return f.states }
func (f *fakeMCP) Probe(ctx context.Context, name string) (mcp.ProbeResult, error) {
	return f.probe(ctx, name)
}
func (f *fakeMCP) Start(context.Context, string) error { return nil }
func (f *fakeMCP) Stop(string) error                   { return nil }

func TestMCPChecks(t *testing.T) {
	t.Parallel()
	b := newBackend(t, nil, config.MCPs{
		"files":  {Type: config.MCPStdio, Command: "files-server"},
		"broken": {Type: config.MCPHttp, URL: "http://127.0.0.1:1/mcp", Headers: map[string]string{"Authorization": "Bearer hdr-secret-abcdef123"}},
		"gone":   {Type: config.MCPStdio, Command: "not-installed"},
	})
	rt := &fakeMCP{
		states: map[string]mcp.ClientInfo{"broken": {State: mcp.StateError, Error: errors.New("dial failed with Bearer hdr-secret-abcdef123")}},
		probe: func(_ context.Context, name string) (mcp.ProbeResult, error) {
			switch name {
			case "files":
				return mcp.ProbeResult{Tools: []string{"read", "write"}, Counts: mcp.Counts{Tools: 2}}, nil
			case "broken":
				return mcp.ProbeResult{}, errors.New("connection refused")
			}
			return mcp.ProbeResult{}, mcp.ErrProbeNeedsAuth
		},
	}
	s := New(Options{})
	s.Add(&MCPAdapter{Backend: b, Runtime: rt, Secrets: s.Secrets(), LookPath: func(name string) (string, error) {
		if name == "not-installed" {
			return "", errors.New("not found")
		}
		return "/bin/" + name, nil
	}})

	broken, ok := s.Get(t.Context(), "mcp:broken")
	require.True(t, ok)
	require.Equal(t, StatusConnectionError, broken.Status)
	require.True(t, broken.Live)
	require.NotContains(t, broken.LastError.String(), "hdr-secret")

	files, err := s.Check(t.Context(), "mcp:files")
	require.NoError(t, err)
	require.Equal(t, StatusReady, files.Status)
	require.Contains(t, files.Detail.String(), "2 tools")

	gone, err := s.Check(t.Context(), "mcp:gone")
	require.NoError(t, err)
	require.Equal(t, StatusMissingDependency, gone.Status)
	require.Contains(t, gone.Remedy.String(), "not-installed")

	require.Len(t, s.Snapshot(t.Context(), Filter{Status: "attention"}), 2)
}

func TestToolProbesShareResultsWithDependencies(t *testing.T) {
	t.Parallel()
	var runs atomic.Int32
	p := NewProber()
	p.LookPath = func(name string) (string, error) {
		if name == "python" {
			return "/usr/bin/python", nil
		}
		return "", errors.New("not found")
	}
	p.Run = func(context.Context, string, ...string) (string, error) {
		runs.Add(1)
		return "Python 3.8.10", nil
	}
	specs := []ToolSpec{
		{ID: "python", Name: "Python", Candidates: []string{"python"}, VersionArgs: []string{"--version"}},
		{ID: "git", Name: "Git", Candidates: []string{"git"}, VersionArgs: []string{"--version"}},
	}
	s := New(Options{})
	s.Add(&ToolAdapter{Specs: specs, Prober: p})
	s.Add(&DependencyAdapter{Runtimes: []Runtime{{Kind: "py", Name: "Python apps", Tool: "python", Constraint: ">=3.9", Remedy: i18n.M("Install Python 3.9.")}}, Tools: specs, Prober: p})

	git, err := s.Check(t.Context(), "tool:git")
	require.NoError(t, err)
	require.Equal(t, StatusMissingDependency, git.Status)
	require.Contains(t, git.Remedy.String(), "Install Git")

	py, err := s.Check(t.Context(), "tool:python")
	require.NoError(t, err)
	require.Equal(t, StatusReady, py.Status)

	// The dependency view reuses the probe and applies its constraint.
	dep, ok := s.Get(t.Context(), "dependency:py")
	require.True(t, ok)
	require.Equal(t, StatusMissingDependency, dep.Status)
	require.Contains(t, dep.LastError.String(), "3.8.10")
	require.Equal(t, int32(1), runs.Load())
}

func TestCheckTimeout(t *testing.T) {
	t.Parallel()
	a := &fakeAdapter{items: []Item{tool("tool:slow")}, parts: map[string]any{}, check: func(ctx context.Context, _ Item) CheckResult {
		<-ctx.Done()
		return CheckResult{Status: StatusReady}
	}}
	s := New(Options{Timeout: 50 * time.Millisecond})
	s.Add(a)
	it, err := s.Check(t.Context(), "tool:slow")
	require.NoError(t, err)
	require.Equal(t, StatusConnectionError, it.Status)
	require.Contains(t, it.LastError.String(), "No answer within")
	require.Zero(t, s.Running())
}

func TestCheckCancelKeepsNoResult(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	a := &fakeAdapter{items: []Item{tool("tool:slow")}, parts: map[string]any{}, check: func(ctx context.Context, _ Item) CheckResult {
		close(started)
		<-ctx.Done()
		return CheckResult{Status: StatusReady}
	}}
	s := New(Options{Timeout: time.Minute})
	s.Add(a)
	done := make(chan error, 1)
	go func() {
		_, err := s.Check(context.Background(), "tool:slow")
		done <- err
	}()
	<-started
	it, _ := s.Get(t.Context(), "tool:slow")
	require.Equal(t, StatusChecking, it.Status)
	require.True(t, s.Cancel("tool:slow"))
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("canceled check did not return")
	}
	it, _ = s.Get(t.Context(), "tool:slow")
	require.Equal(t, StatusUnchecked, it.Status)
	require.True(t, it.CheckedAt.IsZero())
}

func TestStaleResultDoesNotOverwriteNewConfig(t *testing.T) {
	t.Parallel()
	a := &fakeAdapter{items: []Item{tool("tool:x")}, parts: map[string]any{"tool:x": 1}}
	a.check = func(context.Context, Item) CheckResult {
		// The user edits the configuration while the check runs.
		a.setPart("tool:x", 2)
		return CheckResult{Status: StatusReady, Detail: i18n.Raw("old config works")}
	}
	s := New(Options{})
	s.Add(a)
	it, err := s.Check(t.Context(), "tool:x")
	require.NoError(t, err)
	require.Equal(t, StatusUnchecked, it.Status)
	require.Contains(t, it.Stale.String(), "result was discarded")

	// A result for the old configuration is not shown for the new one.
	a.check = func(context.Context, Item) CheckResult { return CheckResult{Status: StatusReady} }
	it, err = s.Check(t.Context(), "tool:x")
	require.NoError(t, err)
	require.Equal(t, StatusReady, it.Status)
	a.setPart("tool:x", 3)
	it, _ = s.Get(t.Context(), "tool:x")
	require.Equal(t, StatusUnchecked, it.Status)
	require.Contains(t, it.Stale.String(), "configuration changed")
}

func TestRestartShowsHistoryNotCurrentState(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "integrations", "checks.json")
	newSvc := func() *Service {
		s := New(Options{StatePath: statePath})
		s.Add(&fakeAdapter{items: []Item{tool("tool:git")}, parts: map[string]any{}, check: func(context.Context, Item) CheckResult {
			return CheckResult{Status: StatusReady, Detail: i18n.M("Version %s at %s", "2.45.0", "/usr/bin/git")}
		}})
		return s
	}
	first := newSvc()
	_, err := first.Check(t.Context(), "tool:git")
	require.NoError(t, err)

	second := newSvc()
	it, ok := second.Get(t.Context(), "tool:git")
	require.True(t, ok)
	require.Equal(t, StatusUnchecked, it.Status)
	require.NotNil(t, it.Previous)
	require.Equal(t, StatusReady, it.Previous.Status)
	require.Equal(t, "Version 2.45.0 at /usr/bin/git", it.Previous.Detail.String())

	// Persisted messages are localized again on display.
	tr := it.Previous.Detail.Localize(func(s string) string {
		if s == "Version %s at %s" {
			return "Sürüm %s, konum %s"
		}
		return s
	})
	require.Equal(t, "Sürüm 2.45.0, konum /usr/bin/git", tr)
}

func TestSupersededCheckIsDropped(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	release := make(chan struct{})
	a := &fakeAdapter{items: []Item{tool("tool:x")}, parts: map[string]any{}}
	a.check = func(ctx context.Context, _ Item) CheckResult {
		if n.Add(1) == 1 {
			select {
			case <-ctx.Done():
			case <-release:
			}
			return CheckResult{Status: StatusConnectionError}
		}
		return CheckResult{Status: StatusReady}
	}
	s := New(Options{})
	s.Add(a)
	first := make(chan error, 1)
	go func() {
		_, err := s.Check(context.Background(), "tool:x")
		first <- err
	}()
	require.Eventually(t, func() bool { return n.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	it, err := s.Check(t.Context(), "tool:x")
	require.NoError(t, err)
	require.Equal(t, StatusReady, it.Status)
	close(release)
	<-first
	it, _ = s.Get(t.Context(), "tool:x")
	require.Equal(t, StatusReady, it.Status)
}

func TestSecretsRedact(t *testing.T) {
	t.Parallel()
	s := &Secrets{}
	s.Add("abc", "custom-secret-value")
	out := s.Redact("custom-secret-value Authorization: Bearer abcdefghijkl api_key=zzz123 sk-ant-0123456789abcdef port abc")
	require.NotContains(t, out, "custom-secret-value")
	require.NotContains(t, out, "abcdefghijkl")
	require.NotContains(t, out, "zzz123")
	require.NotContains(t, out, "sk-ant-0123456789abcdef")
	require.Contains(t, out, "port abc", "short values are never registered")
}

func TestMessageJSONRoundTrip(t *testing.T) {
	t.Parallel()
	m := i18n.Join(i18n.M("Set %s or add a key from /model.", "X"), i18n.M("Defined in %s; edit that file to change it.", "/a"))
	data, err := json.Marshal(m)
	require.NoError(t, err)
	var back i18n.Message
	require.NoError(t, json.Unmarshal(data, &back))
	require.Equal(t, m.String(), back.String())
	require.Equal(t, "Set X or add a key from /model. Defined in /a; edit that file to change it.", back.String())
}
