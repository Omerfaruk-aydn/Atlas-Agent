package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/stretchr/testify/require"
)

func qualitySpec() JourneySpec {
	return JourneySpec{Version: 1, URL: "https://example.test", Cases: []JourneyCase{{Name: "small offline", Width: 390, Height: 844, Steps: []JourneyStep{{Action: "network", Network: "offline"}, {Action: "click", Selector: "#submit", Repeat: 2}, {Action: "assert", Selector: "#result", Condition: "text", Expected: "Done"}}}}}
}

func TestQualityJourneyPreflightsAllCasesBeforeMutation(t *testing.T) {
	t.Parallel()
	spec := qualitySpec()
	spec.Cases = append(spec.Cases, JourneyCase{Name: "invalid later case", Width: 390, Height: 844, Steps: []JourneyStep{{Action: "delete"}}})
	count := 0
	_, err := RunJourney(t.Context(), spec, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		count++
		return fantasy.NewTextResponse(""), nil
	}, "fixture")
	require.Error(t, err)
	require.Zero(t, count)
}

func TestQualityJourneyFailureDoesNotReplayAndAlwaysRestoresOwnedTab(t *testing.T) {
	t.Parallel()
	spec := qualitySpec()
	var actions []string
	clicks := 0
	report, err := RunJourney(t.Context(), spec, func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		actions = append(actions, p.Action)
		switch p.Action {
		case "tabs":
			return fantasy.NewTextResponse(`{"active_tab":"original"}`), nil
		case "tab_new":
			return fantasy.NewTextResponse(`{"active_tab":"owned"}`), nil
		case "click":
			clicks++
			return fantasy.NewTextErrorResponse("target changed"), nil
		case "tab_select":
			require.Equal(t, "original", p.Advanced.TabID)
		case "tab_close":
			require.Equal(t, "owned", p.Advanced.TabID)
		}
		if p.Action == "cdp" || p.Action == "navigate" {
			require.Equal(t, "owned", p.Advanced.ExpectedTabID)
		}
		return fantasy.NewTextResponse(`{}`), nil
	}, "fixture")
	require.NoError(t, err)
	require.False(t, report.Passed)
	require.Equal(t, 1, clicks)
	require.Equal(t, "blocked", report.Cases[0].Steps[2].Status)
	require.Equal(t, []string{"tab_select", "tab_close"}, actions[len(actions)-2:])
	require.Empty(t, report.CleanupErrors)
}

func TestQualityJourneyCancellationCleansUpWithoutCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	report, err := RunJourney(ctx, qualitySpec(), func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		switch p.Action {
		case "tabs":
			return fantasy.NewTextResponse(`{"active_tab":"original"}`), nil
		case "tab_new":
			return fantasy.NewTextResponse(`{"active_tab":"owned"}`), nil
		case "click":
			cancel()
			return fantasy.ToolResponse{}, context.Canceled
		case "tab_select", "tab_close":
			require.NoError(t, ctx.Err())
		}
		return fantasy.NewTextResponse(`{}`), nil
	}, "cancel")
	require.NoError(t, err)
	require.False(t, report.Passed)
	require.Empty(t, report.CleanupErrors)
}

func TestQualityJourneyToolPreservesPermissionStopAndPersistsFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	data, err := json.Marshal(qualitySpec())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "journey.json"), data, 0o600))
	store := engineering.NewStore(t.TempDir())
	count := 0
	tool := NewStressJourneyTool(root, store, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		count++
		resp := fantasy.NewTextErrorResponse("user denied")
		resp.StopTurn = true
		return resp, nil
	})
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "quality-session")
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "denied", Input: `{"manifest":"journey.json"}`})
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	require.Equal(t, 1, count)
	_, saved, err := store.ReadRecord(t.Context(), "quality-flows-"+engineering.Hash("quality-session"), "journey-denied")
	require.NoError(t, err)
	require.Contains(t, string(saved), `"passed":false`)
}

func TestQualityMeasuredChecksCannotPassUnobservedText(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"handover", "stress_journey"} {
		resp := fantasy.NewTextResponse("passed")
		require.False(t, ToolSucceeded(name, resp, nil))
		require.False(t, ToolOutcomeObserved(name, resp, nil))
		resp = fantasy.WithResponseMetadata(resp, MeasuredCheckMetadata{Observed: true, Passed: true})
		require.True(t, ToolSucceeded(name, resp, nil))
		require.True(t, ToolOutcomeObserved(name, resp, nil))
	}
}

func TestQualityReadOnlyAndOwnershipGuards(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	owned := engineering.WithOwnership(engineering.WithScope(ctx, "session", "task"), root, []string{"src"})
	require.NoError(t, checkUndoOwnership(owned, root, []string{"src/feature.go"}))
	require.ErrorContains(t, checkUndoOwnership(owned, root, []string{"other/file.go"}), "ownership")
	readOnly := execution.WithReadOnly(ctx)
	for _, tool := range []fantasy.AgentTool{NewHandoverTool(root, store, nil), NewStressJourneyTool(root, store, nil)} {
		resp, err := tool.Run(readOnly, fantasy.ToolCall{Input: `{"manifest":"unused.json"}`})
		require.NoError(t, err)
		require.True(t, resp.IsError)
		require.Contains(t, resp.Content, "read-only")
	}
	resp, err := NewUndoFeatureTool(root, store, nil).Run(readOnly, fantasy.ToolCall{Input: `{"action":"apply","plan_id":"unused"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "read-only")
}

func TestQualityHandoverRebindsOnlyDisposableChildWorkspace(t *testing.T) {
	t.Parallel()
	root := experimentFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("Run fixture."), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "handover.json"), []byte(`{"version":1,"readme":"README.md","steps":[{"name":"smoke","argv":["fixture"],"readme_quote":"Run fixture.","output_contains":"OK"}]}`), 0o600))
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	ctx = engineering.WithOwnership(engineering.WithScope(ctx, "session", "task"), root, []string{"src"})
	ctx = execution.WithBinding(ctx, execution.Binding{Root: root})
	tool := NewHandoverTool(root, engineering.NewStore(t.TempDir()), func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BashParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		require.NotEqual(t, root, p.WorkingDir)
		require.Equal(t, p.WorkingDir, engineering.GetScope(ctx, "").WriteRoot)
		binding, ok := execution.GetBinding(ctx)
		require.True(t, ok)
		require.Equal(t, p.WorkingDir, binding.Root)
		require.Equal(t, []string{"fixture"}, p.Argv)
		exit := 0
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("OK"), BashResponseMetadata{ExitCode: &exit, Output: "OK", StartTime: 1, EndTime: 2}), nil
	})
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "handover", Input: `{"manifest":"handover.json"}`})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, root, engineering.GetScope(ctx, "").WriteRoot)
	binding, ok := execution.GetBinding(ctx)
	require.True(t, ok)
	require.Equal(t, root, binding.Root)
}

func TestQualityJourneyRealBrowserFaultsAndRestoration(t *testing.T) {
	if testing.Short() {
		t.Skip("real browser excluded")
	}
	exe := ""
	for _, path := range []string{"C:/Program Files/Google/Chrome/Application/chrome.exe", "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "/usr/bin/chromium"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			exe = path
			break
		}
	}
	if exe == "" {
		t.Skip("installed browser unavailable")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" {
			fmt.Fprint(w, "loaded")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><input id="field"><button id="count" onclick="document.querySelector('#number').textContent=++window.count">Count</button><p id="number">0</p><button id="fetch" onclick="document.querySelector('#result').textContent='loading';fetch('/api').then(r=>r.text()).then(t=>document.querySelector('#result').textContent=t).catch(()=>document.querySelector('#result').textContent='error')">Fetch</button><p id="result"></p><script>window.count=0</script>`)
	}))
	defer server.Close()
	manager := browser.GetManager(browser.Options{ExecutablePath: exe, Headless: true, OverlayDisabled: true, UserDataDir: t.TempDir(), IsolateProfiles: true, ActionTimeout: 15 * time.Second})
	defer manager.Close("quality-real")
	tool := newBrowserTool(&mockPermissionService{}, t.TempDir(), manager, browserDescription(false))
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "quality-real")
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return tool.Run(ctx, call)
	}
	spec := JourneySpec{Version: 1, URL: server.URL, Cases: []JourneyCase{
		{Name: "narrow duplicate invalid", Width: 390, Height: 844, Steps: []JourneyStep{{Action: "click", Selector: "#count", Repeat: 2}, {Action: "assert", Selector: "#number", Condition: "text", Expected: "2"}, {Action: "type", Selector: "#field", Text: "invalid email"}, {Action: "assert", Selector: "#field", Condition: "value", Expected: "invalid email"}}},
		{Name: "offline recovery", Width: 1280, Height: 800, Steps: []JourneyStep{{Action: "network", Network: "offline"}, {Action: "click", Selector: "#fetch"}, {Action: "assert", Selector: "#result", Condition: "text", Expected: "error"}, {Action: "network", Network: "online"}, {Action: "click", Selector: "#fetch"}, {Action: "assert", Selector: "#result", Condition: "text", Expected: "loaded"}}},
		{Name: "blocked resource", Width: 1280, Height: 800, BlockedURLs: []string{"*/api"}, Steps: []JourneyStep{{Action: "click", Selector: "#fetch"}, {Action: "assert", Selector: "#result", Condition: "text", Expected: "error"}}},
		{Name: "slow network", Width: 1280, Height: 800, LatencyMS: 150, DownloadBytesPerSecond: 100000, Steps: []JourneyStep{{Action: "click", Selector: "#fetch"}, {Action: "assert", Selector: "#result", Condition: "text", Expected: "loaded"}}},
	}}
	report, err := RunJourney(ctx, spec, invoke, "live")
	require.NoError(t, err)
	encoded, _ := json.Marshal(report)
	require.True(t, report.Passed, string(encoded))
	require.Len(t, report.Cases, 4)
	tabs, err := journeyBrowser(ctx, invoke, "post-tabs", BrowserParams{Action: "tabs"})
	require.NoError(t, err)
	var state struct {
		Tabs []any `json:"tabs"`
	}
	require.NoError(t, json.Unmarshal([]byte(tabs.Content), &state))
	require.Len(t, state.Tabs, 1, "owned test tabs must be closed")
}
