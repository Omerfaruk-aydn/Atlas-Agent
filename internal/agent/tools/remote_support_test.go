package tools

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/stretchr/testify/require"
)

// The tools test binary doubles as the remote program for these tests, so
// they run the same on every platform.
func TestMain(m *testing.M) {
	for _, arg := range os.Args[1:] {
		if mode, ok := strings.CutPrefix(arg, "-remote-support-helper="); ok {
			if mode == "mark" {
				f, err := os.OpenFile("marks.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err == nil {
					_, _ = f.WriteString("ran\n")
					_ = f.Close()
				}
				time.Sleep(1500 * time.Millisecond)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func supportHelper(t *testing.T, mode string) []string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return []string{exe, "-remote-support-helper=" + mode}
}

type supportFixture struct {
	host   *support.Host
	ctl    *support.Controller
	remote support.Remote
	root   string
}

// newSupportFixture pairs two endpoints with separate state directories,
// identities and working folders, then has the owner accept.
func newSupportFixture(t *testing.T, caps []support.Capability, configure ...func(*support.HostOptions)) supportFixture {
	t.Helper()
	root := t.TempDir()
	opts := support.HostOptions{StateDir: t.TempDir(), Name: "owner-pc", LANListen: "127.0.0.1:0", DiscoveryPort: -1, ReconnectWindow: 5 * time.Second}
	for _, configure := range configure {
		configure(&opts)
	}
	h, err := support.StartHost(context.Background(), opts)
	require.NoError(t, err)
	t.Cleanup(h.Close)
	c, err := support.NewController(context.Background(), support.ControllerOptions{StateDir: t.TempDir(), Name: "helper-pc", DiscoveryPort: -1, ReconnectWindow: 5 * time.Second})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	info, err := h.Offer(support.OfferOptions{Capabilities: caps, Roots: []nodes.Root{{Label: "Work", Path: root}}, LAN: true})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := c.Join(context.Background(), info.Code+"@"+h.LANAddress(), nil)
		done <- err
	}()
	require.Eventually(t, func() bool { return h.Status().Pending != nil }, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, h.Decide(h.Status().Pending.ID, true, nil))
	require.NoError(t, <-done)
	r, _, err := c.Target()
	require.NoError(t, err)
	return supportFixture{host: h, ctl: c, remote: r, root: root}
}

// yolo approves everything on the supporter's side, like auto-approve mode.
func yolo(t *testing.T) permission.Service {
	return permission.NewPermissionService(t.TempDir(), true, nil)
}

type supportCall struct {
	resp    fantasy.ToolResponse
	body    map[string]any
	outcome ToolOutcome
}

func callSupport(t *testing.T, tool fantasy.AgentTool, id string, args map[string]any) supportCall {
	t.Helper()
	data, err := json.Marshal(args)
	require.NoError(t, err)
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: id, Name: RemoteSupportToolName, Input: string(data)})
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &body), resp.Content)
	got, ok := ResponseOutcome(resp)
	require.True(t, ok, "every result carries outcome metadata")
	return supportCall{resp: resp, body: body, outcome: got}
}

func errCode(c supportCall) string {
	e, _ := c.body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestRemoteSupportWithoutAConnectedComputerNeverRunsLocally(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	tool := NewRemoteSupportTool(yolo(t), work, func() *support.Controller { return nil })
	got := callSupport(t, tool, "c1", map[string]any{"action": "exec", "device_id": "node-x", "argv": supportHelper(t, "mark")})
	require.True(t, got.resp.IsError)
	require.Equal(t, string(support.CodeNoTarget), errCode(got))
	require.Equal(t, "none", got.outcome.Dispatch)
	require.NoFileExists(t, filepath.Join(work, "marks.txt"))

	// A controller that is not connected is just as unusable.
	c, err := support.NewController(context.Background(), support.ControllerOptions{StateDir: t.TempDir(), DiscoveryPort: -1})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	tool = NewRemoteSupportTool(yolo(t), work, func() *support.Controller { return c })
	got = callSupport(t, tool, "c2", map[string]any{"action": "observe", "device_id": "node-x"})
	require.Equal(t, string(support.CodeNoTarget), errCode(got))
}

// Scenarios 5 and 6: the supporter's auto-approve cannot widen the owner's
// grant, and a call naming another device sends nothing.
func TestRemoteSupportAutoApproveCannotWidenTheGrantOrChangeTarget(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapSystemInfo})
	work := t.TempDir()
	tool := NewRemoteSupportTool(yolo(t), work, func() *support.Controller { return f.ctl })

	got := callSupport(t, tool, "c1", map[string]any{"action": "exec", "device_id": f.remote.RemoteDeviceID, "argv": supportHelper(t, "mark"), "cwd": "Work"})
	require.Equal(t, string(support.CodeCapabilityDenied), errCode(got))
	require.Equal(t, false, got.body["input_sent"])
	require.Equal(t, "none", got.outcome.Dispatch)
	require.NoFileExists(t, filepath.Join(f.root, "marks.txt"))

	got = callSupport(t, tool, "c2", map[string]any{"action": "system_info", "device_id": "node-0000000000000000"})
	require.Equal(t, string(support.CodeWrongTarget), errCode(got))
	got = callSupport(t, tool, "c3", map[string]any{"action": "system_info"})
	require.Equal(t, string(support.CodeWrongTarget), errCode(got), "device_id is mandatory")

	got = callSupport(t, tool, "c4", map[string]any{"action": "system_info", "device_id": f.remote.RemoteDeviceID})
	require.False(t, got.resp.IsError, got.resp.Content)
	device := got.body["device"].(map[string]any)
	require.Equal(t, f.remote.RemoteDeviceID, device["remote_device_id"])
	require.Equal(t, f.remote.SessionID, device["session_id"])
	require.Equal(t, []string{"remote:" + f.remote.RemoteDeviceID}, got.outcome.Subjects)
	require.Empty(t, f.host.Status().Session.Recent, "nothing ran on the remote computer")
}

// Scenario 7: a mutation whose connection dropped is reconciled from the
// remote journal and never sent twice.
func TestRemoteSupportUnknownOutcomeIsReconciledNotResent(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapCommand})
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	marks := filepath.Join(f.root, "marks.txt")
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(marks); err == nil {
				f.ctl.ForceReconnect()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	got := callSupport(t, tool, "c-mark", map[string]any{"action": "exec", "device_id": f.remote.RemoteDeviceID, "argv": supportHelper(t, "mark"), "cwd": "Work"})
	require.True(t, got.resp.IsError)
	require.Equal(t, string(support.CodeOutcomeUnknown), errCode(got), got.resp.Content)
	require.Equal(t, true, got.body["input_sent"])
	require.Equal(t, "unknown", got.outcome.Dispatch)
	require.NotNil(t, got.body["reconciled"], "the journal entry was read after reconnecting: %s", got.resp.Content)
	check := got.body["checkpoint"].(map[string]any)
	require.Equal(t, "operation_status", check["reconcile_with"])
	opID := got.body["operation_id"].(string)

	require.Eventually(t, func() bool {
		st := callSupport(t, tool, "c-status", map[string]any{"action": "operation_status", "device_id": f.remote.RemoteDeviceID, "operation_id": opID})
		op, _ := st.body["result"].(map[string]any)
		return op != nil && op["state"] == "completed"
	}, 15*time.Second, 100*time.Millisecond)
	data, err := os.ReadFile(marks)
	require.NoError(t, err)
	require.Equal(t, "ran\n", string(data), "the command ran exactly once")

	// Repeating the same tool call is answered from the journal.
	again := callSupport(t, tool, "c-mark", map[string]any{"action": "exec", "device_id": f.remote.RemoteDeviceID, "argv": supportHelper(t, "mark"), "cwd": "Work"})
	require.False(t, again.resp.IsError, again.resp.Content)
	result := again.body["result"].(map[string]any)
	require.Equal(t, true, result["replayed"])
	data, _ = os.ReadFile(marks)
	require.Equal(t, "ran\n", string(data))
}

// Scenarios 15 and 16: an unavailable remote desktop or browser never turns
// into local desktop or browser use.
func TestRemoteSupportDesktopAndBrowserStayRemote(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapScreenView, support.CapInput})
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })

	got := callSupport(t, tool, "c1", map[string]any{"action": "observe", "device_id": f.remote.RemoteDeviceID})
	require.Equal(t, string(support.CodeDesktopUnavailable), errCode(got))
	require.Equal(t, f.remote.RemoteDeviceID, got.body["device"].(map[string]any)["remote_device_id"])

	got = callSupport(t, tool, "c2", map[string]any{"action": "launch", "device_id": f.remote.RemoteDeviceID, "argv": []string{"msedge", "https://example.com"}})
	require.Contains(t, []string{string(support.CodeDesktopUnavailable), string(support.CodeCapabilityDenied)}, errCode(got))
	require.Equal(t, "none", got.outcome.Dispatch)

	f2 := newSupportFixture(t, []support.Capability{support.CapScreenView})
	tool = NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f2.ctl })
	got = callSupport(t, tool, "c3", map[string]any{"action": "launch", "device_id": f2.remote.RemoteDeviceID, "argv": []string{"chrome", "https://example.com"}})
	require.Contains(t, []string{string(support.CodeDesktopUnavailable), string(support.CodeCapabilityDenied)}, errCode(got))
	require.False(t, got.body["input_sent"].(bool))
}

// Scenario 18: an installer that exits 0 is not reported as a verified
// installation.
func TestRemoteSupportInstallIsNotReportedAsVerified(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapCommand, support.CapInstall})
	exe, err := os.Executable()
	require.NoError(t, err)
	installer := filepath.Join(t.TempDir(), "app-installer"+filepath.Ext(exe))
	copyFile(t, exe, installer)
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	got := callSupport(t, tool, "c1", map[string]any{"action": "exec", "device_id": f.remote.RemoteDeviceID, "argv": []string{installer, "-remote-support-helper=ok"}, "cwd": "Work"})
	require.False(t, got.resp.IsError, got.resp.Content)
	require.Equal(t, "install_unverified", got.body["verification"])
	require.False(t, got.outcome.Verified)
	require.Contains(t, got.body["note"], "Not verified")

	// Without the install grant the same program is refused before it runs.
	f2 := newSupportFixture(t, []support.Capability{support.CapCommand})
	tool = NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f2.ctl })
	got = callSupport(t, tool, "c2", map[string]any{"action": "exec", "device_id": f2.remote.RemoteDeviceID, "argv": []string{installer, "-remote-support-helper=ok"}})
	require.Equal(t, string(support.CodeCapabilityDenied), errCode(got))
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	require.NoError(t, err)
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	require.NoError(t, err)
	_, err = io.Copy(out, in)
	require.NoError(t, err)
	require.NoError(t, out.Close())
}

func TestRemoteSupportTransfersAreVerified(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapFileRead, support.CapFileSend, support.CapFileReceive})
	work := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(work, "fix.reg.txt"), []byte("settings"), 0o600))
	tool := NewRemoteSupportTool(yolo(t), work, func() *support.Controller { return f.ctl })

	got := callSupport(t, tool, "c1", map[string]any{"action": "push", "device_id": f.remote.RemoteDeviceID, "local_path": "fix.reg.txt", "remote_path": "Work/fix.reg.txt"})
	require.False(t, got.resp.IsError, got.resp.Content)
	require.Equal(t, "verified", got.body["verification"])
	data, err := os.ReadFile(filepath.Join(f.root, "fix.reg.txt"))
	require.NoError(t, err)
	require.Equal(t, "settings", string(data))

	got = callSupport(t, tool, "c2", map[string]any{"action": "pull", "device_id": f.remote.RemoteDeviceID, "remote_path": "Work/fix.reg.txt", "local_path": "back/fix.txt"})
	require.False(t, got.resp.IsError, got.resp.Content)
	data, err = os.ReadFile(filepath.Join(work, "back", "fix.txt"))
	require.NoError(t, err)
	require.Equal(t, "settings", string(data))

	got = callSupport(t, tool, "c3", map[string]any{"action": "push", "device_id": f.remote.RemoteDeviceID, "local_path": "../outside.txt", "remote_path": "Work/x.txt"})
	require.Equal(t, string(support.CodePathDenied), errCode(got))
	got = callSupport(t, tool, "c4", map[string]any{"action": "pull", "device_id": f.remote.RemoteDeviceID, "remote_path": "Work/../../etc/passwd", "local_path": "p.txt"})
	require.Equal(t, string(support.CodePathDenied), errCode(got))
}

// Scenario 17: a pipeline over remote_support keeps the remote target, the
// checkpoint and the outcome of each step.
func TestRemoteSupportPipelineKeepsTargetCheckpointAndOutcome(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapSystemInfo, support.CapCommand})
	remote := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		require.Equal(t, RemoteSupportToolName, call.Name)
		return remote.Run(ctx, call)
	}
	resp, results := runPipelineSteps(t, invoke, PipelineParams{Steps: []PipelineStep{
		{ID: "inspect", Tool: RemoteSupportToolName, Arguments: map[string]any{"action": "system_info", "device_id": f.remote.RemoteDeviceID}},
		{ID: "run", Tool: RemoteSupportToolName, Arguments: map[string]any{"action": "exec", "device_id": f.remote.RemoteDeviceID, "argv": supportHelper(t, "ok"), "cwd": "Work"}},
		{ID: "wrong", Tool: RemoteSupportToolName, Arguments: map[string]any{"action": "exec", "device_id": "node-elsewhere", "argv": supportHelper(t, "mark")}},
	}})
	require.True(t, resp.IsError, "the wrong-device step fails the pipeline")
	got := outcomesByID(results)
	require.Equal(t, OutcomeApplied, got["run"])
	require.NotEqual(t, OutcomeApplied, got["wrong"])
	require.Contains(t, resp.Content, f.remote.RemoteDeviceID)
	require.Contains(t, resp.Content, "checkpoint")
	require.NoFileExists(t, filepath.Join(f.root, "marks.txt"))
	require.True(t, CallMutates(RemoteSupportToolName, `{"action":"exec"}`))
	require.False(t, CallMutates(RemoteSupportToolName, `{"action":"system_info"}`))
}
