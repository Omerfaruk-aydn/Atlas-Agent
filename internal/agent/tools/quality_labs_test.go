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
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/qualityflows"
	"github.com/stretchr/testify/require"
)

func labManifest(t *testing.T, root, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, path), data, 0o600))
}

func TestQualityLabsDatasetGuardedWriteIdempotenceAndOwnership(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	spec := qualityflows.DatasetSpec{Version: 1, Seed: 42, Output: "fixture.json", Entities: []qualityflows.DatasetEntity{{Name: "users", Count: 2, Fields: []qualityflows.DatasetField{{Name: "id", Kind: "id"}}}}}
	labManifest(t, root, "dataset.json", spec)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "lab")
	count := 0
	tool := NewDatasetForgeTool(root, store, func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		count++
		require.Equal(t, WriteToolName, call.Name)
		var p WriteParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		require.Equal(t, filepath.Join(root, spec.Output), p.FilePath)
		require.NoError(t, os.WriteFile(p.FilePath, []byte(p.Content), 0o600))
		return fantasy.NewTextResponse("written"), nil
	})
	call := fantasy.ToolCall{ID: "generate", Name: "dataset_forge", Input: `{"manifest":"dataset.json"}`}
	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.False(t, ToolOutcomeObserved(call.Name, resp, nil))
	require.Equal(t, 1, count)
	call.ID = "repeat"
	resp, err = tool.Run(ctx, call)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Equal(t, 1, count)
	owned := engineering.WithScope(ctx, "lab", "task")
	owned = engineering.WithOwnership(owned, root, []string{"other"})
	call.ID = "ownership"
	resp, err = tool.Run(owned, call)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, 1, count)
}

func TestQualityLabsDatasetDeniedWritePreservesStopTurn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "lab")
	labManifest(t, root, "dataset.json", qualityflows.DatasetSpec{Version: 1, Output: "out.json", Entities: []qualityflows.DatasetEntity{{Name: "items", Count: 1, Fields: []qualityflows.DatasetField{{Name: "id", Kind: "id"}}}}})
	tool := NewDatasetForgeTool(root, store, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		resp := fantasy.NewTextErrorResponse("denied")
		resp.StopTurn = true
		return resp, nil
	})
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "denied", Name: "dataset_forge", Input: `{"manifest":"dataset.json"}`})
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	_, err = os.Stat(filepath.Join(root, "out.json"))
	require.True(t, os.IsNotExist(err))
}

func TestQualityLabsAssumptionsAndBoundaryProduceMeasuredEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "lab")
	require.NoError(t, os.WriteFile(filepath.Join(root, "orders.json"), []byte(`[{"id":1},{"id":2}]`), 0o600))
	labManifest(t, root, "assumptions.json", qualityflows.AssumptionSpec{Version: 1, Checks: []qualityflows.Assumption{{Name: "unique orders", File: "orders.json", Rule: "unique", Field: "/id"}}})
	tool := NewAssumptionCheckTool(root, store)
	call := fantasy.ToolCall{ID: "check", Name: "assumption_check", Input: `{"manifest":"assumptions.json"}`}
	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.True(t, ToolSucceeded(call.Name, resp, nil))
	require.True(t, ToolOutcomeObserved(call.Name, resp, nil))
	labManifest(t, root, "prepare.json", qualityflows.BoundarySpec{Version: 1, Action: "prepare", Labels: []string{"customer"}})
	boundary := NewDataBoundaryTool(root, store)
	resp, err = boundary.Run(ctx, fantasy.ToolCall{ID: "prepare", Name: "data_boundary", Input: `{"manifest":"prepare.json"}`})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.False(t, ToolSucceeded("data_boundary", resp, nil))
	var probe qualityflows.BoundaryProbe
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &probe))
	require.NoError(t, os.WriteFile(filepath.Join(root, "control.txt"), []byte(probe.Markers["customer"]), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "clean.txt"), []byte("public output"), 0o600))
	labManifest(t, root, "scan.json", qualityflows.BoundarySpec{Version: 1, Action: "scan", ProbeID: probe.ID, Targets: []qualityflows.BoundaryTarget{{Path: "control.txt", Expect: "present"}, {Path: "clean.txt", Expect: "absent"}}})
	call = fantasy.ToolCall{ID: "scan", Name: "data_boundary", Input: `{"manifest":"scan.json"}`}
	resp, err = boundary.Run(ctx, call)
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.True(t, ToolSucceeded(call.Name, resp, nil))
	_, _, err = store.ReadRecord(ctx, "quality-flows-"+engineering.Hash("lab"), "data_boundary-scan")
	require.NoError(t, err)
	foreign := context.WithValue(t.Context(), SessionIDContextKey, "other-session")
	resp, err = boundary.Run(foreign, call)
	require.NoError(t, err)
	require.True(t, resp.IsError)
}

func TestQualityTimeLabRejectsMissingClockBeforeBrowserDispatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	labManifest(t, root, "time.json", qualitySpec())
	called := false
	tool := NewTimeLabTool(root, store, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		called = true
		return fantasy.NewTextResponse(""), nil
	})
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "time")
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "time", Name: "time_lab", Input: `{"manifest":"time.json"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.False(t, called)
}

func TestQualityTimeLabRealBrowserTimersAndRestoration(t *testing.T) {
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
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><p id="time"></p><p id="status">active</p><p id="count">0</p><button id="schedule">Schedule</button><script>
document.querySelector('#time').textContent=new Date().toISOString();
let count=0; const interval=setInterval(()=>{document.querySelector('#count').textContent=++count;if(count===3)clearInterval(interval)},1000);
setTimeout(()=>Promise.resolve().then(()=>document.querySelector('#status').textContent='expired'),120000);
document.querySelector('#schedule').onclick=()=>setTimeout(()=>document.querySelector('#status').textContent='clicked',1000);
</script>`)
	}))
	defer server.Close()
	manager := browser.GetManager(browser.Options{ExecutablePath: exe, Headless: true, OverlayDisabled: true, UserDataDir: t.TempDir(), IsolateProfiles: true, ActionTimeout: 15 * time.Second})
	defer manager.Close("quality-time-real")
	browserTool := newBrowserTool(&mockPermissionService{}, t.TempDir(), manager, browserDescription(false))
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "quality-time-real")
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return browserTool.Run(ctx, call)
	}
	spec := JourneySpec{Version: 1, URL: server.URL, Clock: &JourneyClock{Start: "2026-12-31T23:59:00Z", Timezone: "Europe/Istanbul"}, Cases: []JourneyCase{{Name: "session expiry and live interaction", Width: 1280, Height: 800, Steps: []JourneyStep{
		{Action: "assert", Selector: "#time", Condition: "text", Expected: "2026-12-31T23:59:00.000Z"},
		{Action: "advance_time", AdvanceMS: 120000},
		{Action: "assert", Selector: "#count", Condition: "text", Expected: "3"},
		{Action: "assert", Selector: "#status", Condition: "text", Expected: "expired"},
		{Action: "click", Selector: "#schedule"},
		{Action: "advance_time", AdvanceMS: 1000},
		{Action: "assert", Selector: "#status", Condition: "text", Expected: "clicked"},
	}}}}
	report, err := RunJourney(ctx, spec, invoke, "live-time")
	require.NoError(t, err)
	encoded, _ := json.Marshal(report)
	require.True(t, report.Passed, string(encoded))
	resp, err := journeyBrowser(ctx, invoke, "restored", BrowserParams{Action: "eval", Script: "({now:Date.now(),clock:typeof globalThis.__atlasTimeLab})"})
	require.NoError(t, err)
	var restored struct {
		Now   int64  `json:"now"`
		Clock string `json:"clock"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &restored))
	require.Equal(t, "undefined", restored.Clock)
	require.InDelta(t, time.Now().UnixMilli(), restored.Now, 5000)
}
