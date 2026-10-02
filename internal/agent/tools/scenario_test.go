package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/scenarios"
	"github.com/stretchr/testify/require"
)

func TestScenarioTerminalChild(t *testing.T) {
	if os.Getenv("AI_AGENT") != "atlas-terminal" {
		return
	}
	fmt.Println("SCENARIO_READY")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if scanner.Text() == "quit" {
			fmt.Println("SCENARIO_EXIT")
			os.Exit(0)
		}
		fmt.Printf("ANSWER=%s\n", scanner.Text())
	}
	os.Exit(87)
}

func TestScenarioRealTerminalPersistsProofAndDeniedRunNeverStarts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, initErr := engineering.Git(t.Context(), root, "init")
	require.NoError(t, initErr)
	store := engineering.NewStore(filepath.Join(root, ".atlas"))
	exe, err := os.Executable()
	require.NoError(t, err)
	s := scenarios.Scenario{ID: "terminal", Version: 1, Target: "tui", Argv: []string{exe, "-test.run=^TestScenarioTerminalChild$"}, Width: 80, Height: 24, TimeoutMS: 10000, Steps: []scenarios.Step{{Action: "wait_text", Value: "SCENARIO_READY"}, {Action: "input", Value: "hello\r"}, {Action: "wait_text", Value: "ANSWER=hello"}, {Action: "resize", Width: 120, Height: 40}, {Action: "input", Value: "quit\r"}}, Assertions: []scenarios.Assertion{{Kind: "transcript", Expected: "SCENARIO_EXIT"}, {Kind: "exit", Expected: "0"}, {Kind: "status", Expected: "succeeded"}}}
	services := ScenarioServices{Root: root, SessionID: "scenario-session", Store: store, Approve: func(_ context.Context, req permission.CreatePermissionRequest) (bool, error) {
		require.Equal(t, s.Argv, req.Params.(BashPermissionsParams).Argv)
		return true, nil
	}}
	run, err := ExecuteScenario(t.Context(), services, s, fantasy.ToolCall{ID: "real", Input: `{}`})
	require.NoError(t, err)
	require.True(t, run.Passed, "%+v", run)
	require.True(t, run.Observed)
	var raw, rendering bool
	for _, artifact := range run.Artifacts {
		raw = raw || artifact.Kind == "pty-transcript"
		rendering = rendering || artifact.Kind == "terminal-rendering"
	}
	require.True(t, raw)
	require.True(t, rendering)
	state, err := store.Read(t.Context(), services.SessionID)
	require.NoError(t, err)
	require.Len(t, state.UIEvidence, 1)
	require.NoError(t, engineering.ValidateUIArtifact(t.Context(), state.UIEvidence[0]))
	forged := state.UIEvidence[0]
	forged.ID = "wrong-run"
	require.ErrorContains(t, engineering.ValidateUIArtifact(t.Context(), forged), "identity")
	require.True(t, strings.HasPrefix(run.Result.Backend, "pty-"))
	saved, _, err := ReadScenario(t.Context(), services, run.ID)
	require.NoError(t, err)
	require.Equal(t, run.ID, saved.Run.ID)
	require.NoError(t, scenarios.Current(t.Context(), saved.Run, &scenarioBackend{ScenarioServices: services}))
	services.Approve = func(context.Context, permission.CreatePermissionRequest) (bool, error) { return false, nil }
	denied, err := ExecuteScenario(t.Context(), services, s, fantasy.ToolCall{ID: "denied", Input: `{}`})
	require.ErrorContains(t, err, "denied")
	require.False(t, denied.Observed)
	require.Empty(t, denied.Artifacts)
	services.Approve = func(context.Context, permission.CreatePermissionRequest) (bool, error) {
		t.Fatal("Blocked command must not reach approval or execution")
		return true, nil
	}
	services.CommandPolicy = CommandPolicy{Block: []string{filepath.Base(exe)}}
	blocked, err := ExecuteScenario(t.Context(), services, s, fantasy.ToolCall{ID: "blocked", Input: `{}`})
	require.ErrorContains(t, err, "blocked")
	require.False(t, blocked.Observed)
	require.NoError(t, os.WriteFile(filepath.Join(root, "changed.txt"), []byte("changed"), 0o644))
	require.ErrorContains(t, scenarios.Current(t.Context(), saved.Run, &scenarioBackend{ScenarioServices: services}), "source changed")
}

func TestScenarioRealBrowserPersistsAcrossReload(t *testing.T) {
	if testing.Short() {
		t.Skip("Real browser fixture excluded by short mode")
	}
	executable := ""
	for _, path := range []string{"C:/Program Files/Google/Chrome/Application/chrome.exe", "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "/usr/bin/chromium", "/usr/bin/google-chrome"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			executable = path
			break
		}
	}
	if executable == "" {
		t.Skip("Real browser unavailable; this is not passing browser proof")
	}
	root := t.TempDir()
	_, initErr := engineering.Git(t.Context(), root, "init")
	require.NoError(t, initErr)
	store := engineering.NewStore(filepath.Join(root, ".atlas"))
	var pageRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			pageRequests.Add(1)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><input id="value"><button id="save" onclick="localStorage.setItem('saved',document.querySelector('#value').value)">Save</button><script>document.querySelector('#value').value=localStorage.getItem('saved')||''</script>`)
	}))
	defer server.Close()
	manager := browser.GetManager(browser.Options{ExecutablePath: executable, Headless: true, UserDataDir: t.TempDir(), ActionTimeout: 10 * time.Second})
	defer manager.Close("scenario-browser")
	tool := newBrowserTool(&mockBashPermissionService{}, root, manager, "test")
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "scenario-browser")
	services := ScenarioServices{Root: root, SessionID: "scenario-browser", Store: store, Invoke: tool.Run}
	s := scenarios.Scenario{ID: "browser", Version: 1, Target: "web", URL: server.URL, Width: 800, Height: 600, TimeoutMS: 30000, Steps: []scenarios.Step{{Action: "type", Selector: "#value", Value: "persisted"}, {Action: "click", Selector: "#save"}, {Action: "reload"}}, Assertions: []scenarios.Assertion{{Kind: "value", Selector: "#value", Expected: "persisted"}, {Kind: "visible", Selector: "#save"}}}
	run, err := ExecuteScenario(ctx, services, s, fantasy.ToolCall{ID: "browser-real", Input: `{}`})
	require.NoError(t, err)
	require.True(t, run.Passed, "%+v", run)
	require.Len(t, run.Artifacts, 3)
	require.GreaterOrEqual(t, pageRequests.Load(), int64(2), "Reload must actually request a new document")
	saved, record, err := ReadScenario(ctx, services, run.ID)
	require.NoError(t, err)
	require.Greater(t, record.Revision, uint64(1))
	encoded, err := json.Marshal(saved)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "data:image")
	// Close the actual browser process and reopen its retained profile. This
	// verifies persistence after restart, beyond reloading a document.
	manager.Close("scenario-browser")
	s.ID = "browser-restarted"
	s.Steps = nil
	restarted, err := ExecuteScenario(ctx, services, s, fantasy.ToolCall{ID: "browser-restarted", Input: `{}`})
	require.NoError(t, err)
	require.True(t, restarted.Passed, "%+v", restarted)
}

func TestScenarioToolRejectsUnknownFieldsBeforeExecution(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	tool := NewScenarioTool(t.TempDir(), store, nil, nil)
	response, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "invalid", Input: `{"action":"run","scenario":{"id":"fake","version":1,"target":"web","url":"http://localhost:3000","width":800,"height":600,"steps":[],"assertions":[{"kind":"visible","selector":"body"}],"passed":true}}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "unknown field")
	state, err := store.Read(t.Context(), "unused")
	require.NoError(t, err)
	require.Empty(t, state.Operations)
}
