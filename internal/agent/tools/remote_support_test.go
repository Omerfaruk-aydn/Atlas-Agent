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

