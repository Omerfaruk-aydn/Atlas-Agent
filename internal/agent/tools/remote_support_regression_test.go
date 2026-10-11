package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/stretchr/testify/require"
)

func TestRemoteSupportFailedNativeOperationsAreErrors(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"input", "launch"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f := newSupportFixture(t, []support.Capability{support.CapInput})
			_, client, err := f.ctl.Target()
			require.NoError(t, err)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(nodes.Operation{ID: "op-test", State: "failed", Effect: "unknown", Error: "backend failed"})
			}))
			defer srv.Close()
			client.BaseURL, client.HTTP = srv.URL, srv.Client()
			got := callSupport(t, NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl }), "native-failure", map[string]any{"action": action, "device_id": f.remote.RemoteDeviceID, "argv": []string{"fixture"}, "input_action": "key", "key": "escape"})
			require.True(t, got.resp.IsError)
			require.Equal(t, false, got.body["ok"])
			require.Equal(t, "unknown", got.outcome.Dispatch)
			require.Equal(t, string(support.CodeOutcomeUnknown), errCode(got))
		})
	}
}

func TestRemoteSupportStatusRequiresLiveRead(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapSystemInfo})
	_, client, err := f.ctl.Target()
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(nodes.APIError{Code: "connection_lost", Message: "unavailable", Effect: "none"})
	}))
	defer srv.Close()
	client.BaseURL, client.HTTP = srv.URL, srv.Client()
	got := callSupport(t, NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl }), "status", map[string]any{"action": "status"})
	require.True(t, got.resp.IsError)
	require.Equal(t, "not_verified", got.body["verification"])
	view := got.body["result"].(map[string]any)
	require.Equal(t, true, view["stale"])
	require.Equal(t, "unavailable", view["state"])
}

func TestRemoteSupportObservationDeliversImagePart(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{size: computer.Size{Width: 40, Height: 40}, screenshot: testPNG(t, 40, 40)}
	f := newSupportFixture(t, []support.Capability{support.CapScreenView}, func(o *support.HostOptions) { o.Desktop = backend })
	got := callSupport(t, NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl }), "image", map[string]any{"action": "observe", "device_id": f.remote.RemoteDeviceID, "image": true})
	require.False(t, got.resp.IsError)
	require.Equal(t, "image", got.resp.Type)
	require.Equal(t, "image/png", got.resp.MediaType)
	require.Equal(t, backend.screenshot, got.resp.Data)
	require.NotContains(t, got.resp.Content, "png_base64")
	require.Contains(t, got.resp.Content, "observation_id")
}

func TestRemoteSupportTreeAliasUsesInspect(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapScreenView})
	_, client, err := f.ctl.Target()
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request nodes.AutomationRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		if request.Action != "inspect" {
			http.Error(w, "invalid action", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	defer srv.Close()
	client.BaseURL, client.HTTP = srv.URL, srv.Client()
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	for _, query := range []string{"tree", "inspect"} {
		got := callSupport(t, tool, "tree", map[string]any{"action": "automation", "device_id": f.remote.RemoteDeviceID, "query": query})
		require.False(t, got.resp.IsError, got.resp.Content)
	}
}

func TestRemoteSupportReconcileRejectsDifferentSession(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapSystemInfo})
	_, client, err := f.ctl.Target()
	require.NoError(t, err)
	read := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read = true
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	client.BaseURL, client.HTTP = srv.URL, srv.Client()
	original := deviceOf(f.remote)
	original.SessionID = "ss-another-session"
	tool := &remoteSupportTool{reconcileWait: time.Second}
	got := tool.reconcile(t.Context(), f.ctl, supportResult{Device: original}, support.Errorf(support.CodeOutcomeUnknown, "lost"), "op-original")
	require.False(t, read)
	require.Equal(t, original, got.Device)
	require.Equal(t, support.EffectUnknown, got.Effect)
}

func TestRemoteSupportNativeMetadataSurvives(t *testing.T) {
	t.Parallel()
	res := supportResult{OK: false, Action: "tool", Device: supportDevice{RemoteDeviceID: "node-original"}, Image: testPNG(t, 2, 2), StopTurn: true, NativeMetadata: `{"question":{"id":"q1"},"waiting_for_user":true,"atlas_outcome":{"dispatch":"none"}}`}
	resp := (&remoteSupportTool{}).respond(res, true)
	require.True(t, resp.StopTurn)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Metadata, `"question"`)
	require.Contains(t, resp.Metadata, `"child_atlas_outcome"`)
	require.Equal(t, "image", resp.Type)
	require.Equal(t, res.Image, resp.Data)
}

type supportUnknownExecutor struct{}

func (supportUnknownExecutor) Validate(nodes.RemoteToolRequest) (bool, error) {
	return true, nil
}

func (supportUnknownExecutor) Run(context.Context, nodes.Grant, nodes.RemoteToolRequest, func(nodes.Need) error) (fantasy.ToolResponse, error) {
	return withToolOutcome(fantasy.NewTextResponse("The effect is unknown"), ToolOutcome{Dispatch: "unknown", Mutation: true}), nil
}

func (supportUnknownExecutor) Close(string) {}

func TestRemoteSupportNativeUnknownCannotBecomeCompleted(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapInput}, func(o *support.HostOptions) { o.Executor = supportUnknownExecutor{} })
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	got := callSupport(t, tool, "unknown-native", map[string]any{"action": "tool", "tool": "computer", "device_id": f.remote.RemoteDeviceID, "session_id": f.remote.SessionID, "generation": f.remote.Generation, "arguments": map[string]any{"action": "key", "key": "escape"}})
	require.True(t, got.resp.IsError)
	require.Equal(t, "unknown", got.outcome.Dispatch)
	_, client, err := f.ctl.Target()
	require.NoError(t, err)
	op, err := client.Operation(t.Context(), got.body["operation_id"].(string))
	require.NoError(t, err)
	require.Equal(t, "failed", string(op.State))
	require.Equal(t, "unknown", op.Effect)
}

func TestRemoteSupportHostWorkflowUsesOnlyRemoteBackend(t *testing.T) {
	backend := &fakeComputerBackend{size: computer.Size{Width: 40, Height: 40}}
	f := newSupportFixture(t, []support.Capability{support.CapScreenView, support.CapInput}, func(o *support.HostOptions) {
		o.Desktop = backend
		o.Executor = NewSupportExecutor(o.StateDir, backend, config.ToolBrowser{})
	})
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	steps := make([]map[string]any, 24)
	for i := range steps {
		steps[i] = map[string]any{"id": strings.Repeat("s", i+1), "tool": "computer", "arguments": map[string]any{"action": "key", "key": "escape"}}
	}
	args := map[string]any{"action": "tool", "tool": "tool_pipeline", "device_id": f.remote.RemoteDeviceID, "session_id": f.remote.SessionID, "generation": f.remote.Generation, "arguments": map[string]any{"steps": steps}}
	got := callSupport(t, tool, "workflow", args)
	require.False(t, got.resp.IsError, got.resp.Content)
	require.Len(t, backend.keys, 24)
	// Replaying a completed operation returns the journal, not more input.
	again := callSupport(t, tool, "workflow", args)
	require.False(t, again.resp.IsError, again.resp.Content)
	require.Len(t, backend.keys, 24)
	require.NoError(t, f.host.PauseInput(true))
	denied := callSupport(t, tool, "paused", args)
	require.True(t, denied.resp.IsError)
	require.Len(t, backend.keys, 24)
	args["session_id"] = "ss-older-session"
	stale := callSupport(t, tool, "stale", args)
	require.Equal(t, string(support.CodeStaleConnection), errCode(stale))
	require.Len(t, backend.keys, 24)
}

func TestSupportExecutorRefusesForeignTools(t *testing.T) {
	t.Parallel()
	e := NewSupportExecutor(t.TempDir(), nil, config.ToolBrowser{})
	for _, req := range []nodes.RemoteToolRequest{
		{Tool: "bash", Input: json.RawMessage(`{"command":"anything"}`)},
		{Tool: "browser", Input: json.RawMessage(`{"action":"eval","script":"anything"}`)},
		{Tool: "tool_pipeline", Input: json.RawMessage(`{"steps":[{"id":"x","tool":"bash","arguments":{}}]}`)},
		{Tool: "tool_pipeline", Input: json.RawMessage(`{"steps":[{"id":"first","tool":"computer","arguments":{"action":"key","key":"escape"}},{"id":"later","tool":"browser","arguments":{"action":"eval","script":"anything"}}]}`)},
		{Tool: "tool_pipeline", Input: json.RawMessage(`{"steps":[{"id":"x","tool":"browser","arguments":{"action":"observe"},"bindings":{"action":"earlier#/value"}}]}`)},
		{Tool: "tool_pipeline", Input: json.RawMessage(`{"browser":{"steps":[{"id":"x","action":"upload","target":{"selector":"input"}}]}}`)},
	} {
		_, err := e.Validate(req)
		require.Error(t, err)
	}
	_, err := e.Run(context.Background(), nodes.Grant{SessionID: "ss-test"}, nodes.RemoteToolRequest{Tool: "computer", Input: json.RawMessage(`{"action":"windows"}`)}, nil)
	require.Error(t, err)
}

func TestSupportExecutorKeepsRuntimeStateOutsideExposedDrives(t *testing.T) {
	t.Parallel()
	stateDir, drive := t.TempDir(), t.TempDir()
	backend := &fakeComputerBackend{size: computer.Size{Width: 40, Height: 40}}
	e := NewSupportExecutor(stateDir, backend, config.ToolBrowser{})
	id := fmt.Sprintf("scope-%p", e)
	t.Cleanup(func() { e.Close(id) })
	resp, err := e.Run(t.Context(), nodes.Grant{SessionID: id, Roots: []nodes.Root{{Label: "Drive", Path: drive}}}, nodes.RemoteToolRequest{OperationID: "read-screen", Tool: "computer", Input: json.RawMessage(`{"action":"screen_size"}`)}, func(nodes.Need) error { return nil })
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.DirExists(t, filepath.Join(stateDir, ".atlas", "interactions"))
	require.NoDirExists(t, filepath.Join(drive, ".atlas"))
}

func TestRemoteSupportInstallRequiresIndependentEvidence(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapCommand, support.CapInstall})
	_, client, err := f.ctl.Target()
	require.NoError(t, err)
	zero := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op := nodes.Operation{ID: "op-install", SessionID: f.remote.SessionID, State: "completed", Effect: "applied", Argv: []string{"winget", "install", "Example.App"}, ExitCode: &zero}
		if r.Method == http.MethodPost {
			var req nodes.ExecRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			op.ID, op.Argv = req.OperationID, req.Argv
			if req.Argv[0] == "correct-probe" {
				op.Stdout = "Example.App 1.2.3"
			}
		}
		_ = json.NewEncoder(w).Encode(op)
	}))
	defer srv.Close()
	client.BaseURL, client.HTTP = srv.URL, srv.Client()
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	for _, probe := range []string{"empty-probe", "correct-probe"} {
		got := callSupport(t, tool, probe, map[string]any{"action": "verify_install", "device_id": f.remote.RemoteDeviceID, "operation_id": "op-install", "verify_argv": []string{probe}, "expected_output": "Example.App 1.2.3"})
		require.Equal(t, probe == "empty-probe", got.resp.IsError, got.resp.Content)
		require.Equal(t, probe == "correct-probe", got.outcome.Verified)
	}
}

func TestRemoteSupportLiveBrowserWorkflow(t *testing.T) {
	executable := liveBrowserExecutable(t)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body><label for="name">Name</label><input id="name" value="old"><button>Save</button></body></html>`)
	}))
	t.Cleanup(page.Close)
	headless := true
	f := newSupportFixture(t, []support.Capability{support.CapBrowser}, func(o *support.HostOptions) {
		o.Executor = NewSupportExecutor(o.StateDir, nil, config.ToolBrowser{Headless: &headless, ExecutablePath: executable})
	})
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	args := map[string]any{
		"action": "tool", "tool": "tool_pipeline", "device_id": f.remote.RemoteDeviceID, "session_id": f.remote.SessionID, "generation": f.remote.Generation,
		"arguments": map[string]any{"browser": map[string]any{"steps": []map[string]any{{"id": "open", "action": "navigate", "url": page.URL}, {"id": "write", "action": "set_field", "target": map[string]any{"label": "Name", "value": "remote verified", "commit": "blur"}}, {"id": "read", "action": "read_field", "target": map[string]any{"label": "Name"}}}}},
	}
	got := callSupport(t, tool, "live-browser", args)
	require.False(t, got.resp.IsError, got.resp.Content)
	require.Contains(t, got.resp.Content, "remote verified")
	args["tool"], args["arguments"] = "browser", map[string]any{"action": "screenshot"}
	shot := callSupport(t, tool, "live-screen", args)
	require.False(t, shot.resp.IsError, shot.resp.Content)
	require.Equal(t, "image", shot.resp.Type)
	require.NotEmpty(t, shot.resp.Data)
	require.False(t, shot.outcome.Mutation, "a remote observation must not invalidate existing change evidence")
	require.NoError(t, f.host.PauseInput(true))
	args["arguments"] = map[string]any{"action": "set_field", "advanced": map[string]any{"label": "Name", "value": "denied"}}
	denied := callSupport(t, tool, "live-paused", args)
	require.True(t, denied.resp.IsError, denied.resp.Content)
	args["arguments"] = map[string]any{"action": "read_field", "advanced": map[string]any{"label": "Name"}}
	read := callSupport(t, tool, "live-read", args)
	require.False(t, read.resp.IsError, read.resp.Content)
	require.Contains(t, read.resp.Content, "remote verified")
	require.NotContains(t, read.resp.Content, "denied")
}

func TestRemoteSupportNestedMutationClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tool, input string
		mutates     bool
	}{
		{"browser", `{"action":"observe"}`, false},
		{"browser", `{"action":"set_field","advanced":{"label":"Name","value":"new"}}`, true},
		{"computer", `{"action":"windows"}`, false},
		{"computer", `{"action":"key","key":"escape"}`, true},
		{"tool_pipeline", `{"help":"desktop_flow"}`, false},
	} {
		input := fmt.Sprintf(`{"action":"tool","tool":%q,"arguments":%s}`, tc.tool, tc.input)
		require.Equal(t, tc.mutates, CallMutates(RemoteSupportToolName, input), input)
	}
}

func TestRemoteSupportDurableDesktopFlow(t *testing.T) {
	d := newSimDesktop()
	user := d.open("other", "Rapor.docx - Word", 7777, "WINWORD.EXE", "OpusApp")
	folder := d.open("explorer", "Atlas-Test", 4100, "explorer.exe", "CabinetWClass")
	folder.folder = d.desktopDir + `\Atlas-Test`
	d.foreground = user.info.ID
	f := newSupportFixture(t, []support.Capability{support.CapScreenView, support.CapInput}, func(o *support.HostOptions) {
		o.Desktop = d
		o.Executor = NewSupportExecutor(o.StateDir, d, config.ToolBrowser{})
	})
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	args := map[string]any{
		"action": "tool", "tool": "tool_pipeline", "device_id": f.remote.RemoteDeviceID, "session_id": f.remote.SessionID, "generation": f.remote.Generation,
		"arguments": map[string]any{"desktop": map[string]any{"mode": "prepare", "application": "Hesap Makinesi", "observation": "semantic"}},
	}
	prepare := callSupport(t, tool, "prepare-native", args)
	require.False(t, prepare.resp.IsError, prepare.resp.Content)
	var plan map[string]any
	require.NoError(t, json.Unmarshal([]byte(acceptanceWorkflow), &plan))
	args["arguments"] = plan
	got := callSupport(t, tool, "native-flow", args)
	require.False(t, got.resp.IsError, got.resp.Content)
	native := got.body["result"].(map[string]any)["content"].(string)
	require.Len(t, flowReportOf(t, native).Flow.Nodes, 23)
	d.mu.Lock()
	require.Equal(t, acceptanceNote+"\nKontrol: 9000", d.files[folder.folder+`\sonuc.txt`])
	inputs := len(d.inputs)
	require.NotNil(t, d.window(user.info.ID))
	d.mu.Unlock()
	plan["desktop"].(map[string]any)["flow"].(map[string]any)["resume"] = true
	resumed := callSupport(t, tool, "native-resume", args)
	require.False(t, resumed.resp.IsError, resumed.resp.Content)
	d.mu.Lock()
	require.Len(t, d.inputs, inputs, "completed native flow must not send inputs again")
	d.mu.Unlock()
}
