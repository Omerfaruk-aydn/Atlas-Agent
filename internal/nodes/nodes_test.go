package nodes

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	switch os.Getenv("NODES_HELPER") {
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "echo":
		_, _ = os.Stdout.WriteString("hello from node\n")
		os.Exit(0)
	case "fail":
		os.Exit(3)
	}
	os.Exit(m.Run())
}

type harness struct {
	server *Server
	http   *httptest.Server
	root   string
}

func newHarness(t *testing.T, caps []Capability, desktop computer.Backend) *harness {
	t.Helper()
	root := t.TempDir()
	srv, err := NewServer(ServerOptions{StateDir: t.TempDir(), Root: root, Name: "test-node", Capabilities: caps, Desktop: desktop, DesktopReason: "no session"})
	require.NoError(t, err)
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.TLS = srv.TLSConfig()
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return &harness{server: srv, http: ts, root: root}
}

func (h *harness) pair(t *testing.T) *Client {
	t.Helper()
	code, _ := h.server.Auth().NewPairingCode()
	c, err := NewClient(h.http.URL, h.server.Identity().Fingerprint, "")
	require.NoError(t, err)
	resp, err := c.Pair(t.Context(), code, "controller")
	require.NoError(t, err)
	c.Token = resp.Token
	return c
}

func helperArgv(t *testing.T, mode string) ([]string, func()) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	old, had := os.LookupEnv("NODES_HELPER")
	require.NoError(t, os.Setenv("NODES_HELPER", mode))
	return []string{exe, "-test.run=^$"}, func() {
		if had {
			os.Setenv("NODES_HELPER", old)
		} else {
			os.Unsetenv("NODES_HELPER")
		}
	}
}

func apiCode(err error) string {
	var api *APIError
	if errors.As(err, &api) {
		return api.Code
	}
	return ""
}

func TestPairingCodeIsSingleUseAndExpires(t *testing.T) {
	h := newHarness(t, []Capability{CapExec}, nil)
	c, err := NewClient(h.http.URL, h.server.Identity().Fingerprint, "")
	require.NoError(t, err)
	code, _ := h.server.Auth().NewPairingCode()
	_, err = c.Pair(t.Context(), code, "a")
	require.NoError(t, err)
	_, err = c.Pair(t.Context(), code, "b")
	require.Equal(t, "pairing_invalid", apiCode(err), "a reused code must fail")

	expired, _ := h.server.Auth().NewPairingCode()
	h.server.Auth().now = func() time.Time { return time.Now().Add(pairingTTL + time.Second) }
	_, err = c.Pair(t.Context(), expired, "c")
	require.Equal(t, "pairing_invalid", apiCode(err))
	h.server.Auth().now = time.Now

	_, err = c.Pair(t.Context(), "garbage", "d")
	require.Equal(t, "pairing_invalid", apiCode(err))
}

func TestPairingLocksAfterRepeatedFailures(t *testing.T) {
	a, err := LoadAuth(t.TempDir())
	require.NoError(t, err)
	good, _ := a.NewPairingCode()
	for range maxPairingFailures {
		_, _, err = a.Pair("WRONG-CODE", "x")
		require.Error(t, err)
	}
	_, _, err = a.Pair(good, "x")
	require.ErrorIs(t, err, ErrPairingLocked, "a valid code must not work once locked")
}

func TestWrongFingerprintIsRefusedBeforeAnyRequest(t *testing.T) {
	h := newHarness(t, []Capability{CapExec}, nil)
	c, err := NewClient(h.http.URL, "sha256:"+string(bytes.Repeat([]byte("0"), 64)), "")
	require.NoError(t, err)
	_, err = c.Hello(t.Context())
	var pin *PinError
	require.ErrorAs(t, err, &pin)
}

func TestUnauthorizedAndRevokedDevicesAreRejected(t *testing.T) {
	h := newHarness(t, []Capability{CapExec}, nil)
	c := h.pair(t)
	_, err := c.Info(t.Context())
	require.NoError(t, err)

	bad := *c
	bad.Token = NewToken()
	_, err = bad.Info(t.Context())
	require.Equal(t, "unauthorized", apiCode(err))

	require.NoError(t, c.Revoke(t.Context()))
	_, err = c.Info(t.Context())
	require.Equal(t, "unauthorized", apiCode(err), "a revoked token must stop working immediately")

	// Revocation persists across a node restart.
	auth, err := LoadAuth(h.server.opts.StateDir)
	require.NoError(t, err)
	_, err = auth.Authenticate(c.Token)
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestCapabilityNotGrantedIsDenied(t *testing.T) {
	h := newHarness(t, []Capability{CapFiles}, nil)
	c := h.pair(t)
	_, err := c.Exec(t.Context(), ExecRequest{OperationID: "op-00000001", Argv: []string{"x"}})
	require.Equal(t, "capability_denied", apiCode(err))
	_, err = c.Observe(t.Context(), false)
	require.Equal(t, "capability_denied", apiCode(err))
}

func TestRepeatedOperationIsReplayedNotRerun(t *testing.T) {
	h := newHarness(t, []Capability{CapExec}, nil)
	c := h.pair(t)
	argv, restore := helperArgv(t, "echo")
	defer restore()
	op, err := c.Exec(t.Context(), ExecRequest{OperationID: "op-dedupe-1", Argv: argv})
	require.NoError(t, err)
	require.Equal(t, taskstate.Completed, op.State)
	require.Contains(t, op.Stdout, "hello from node")
	require.False(t, op.Replayed)

	again, err := c.Exec(t.Context(), ExecRequest{OperationID: "op-dedupe-1", Argv: argv})
	require.NoError(t, err)
	require.True(t, again.Replayed, "a duplicate operation_id must return the journal entry")
	require.Equal(t, op.StartedAt, again.StartedAt)

	_, err = c.Exec(t.Context(), ExecRequest{OperationID: "op-dedupe-1", Argv: append(argv, "different")})
	require.Equal(t, "operation_conflict", apiCode(err))
}

func TestFailedCommandReportsExitCode(t *testing.T) {
	h := newHarness(t, []Capability{CapExec}, nil)
	c := h.pair(t)
	argv, restore := helperArgv(t, "fail")
	defer restore()
	op, err := c.Exec(t.Context(), ExecRequest{OperationID: "op-fail-0001", Argv: argv})
	require.NoError(t, err)
	require.Equal(t, taskstate.Failed, op.State)
	require.Equal(t, 3, *op.ExitCode)
}

func TestCancelIsTerminalAndLateResultDoesNotRevive(t *testing.T) {
	h := newHarness(t, []Capability{CapExec}, nil)
	c := h.pair(t)
	argv, restore := helperArgv(t, "sleep")
	defer restore()
	wait := false
	op, err := c.Exec(t.Context(), ExecRequest{OperationID: "op-cancel-01", Argv: argv, Wait: &wait})
	require.NoError(t, err)
	require.Equal(t, taskstate.Running, op.State)
	canceled, err := c.Cancel(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, taskstate.Canceled, canceled.State)
	require.Eventually(t, func() bool {
		h.server.mu.Lock()
		defer h.server.mu.Unlock()
		return h.server.procs[op.ID] == nil
	}, 10*time.Second, 50*time.Millisecond)
	final, err := c.Operation(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, taskstate.Canceled, final.State, "the supervisor's exit must not overwrite cancellation")
}

func TestJournalClosesInterruptedOperationsAsUnknown(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	require.NoError(t, err)
	_, _, err = j.Begin(Operation{ID: "op-crash-001", Kind: "exec", DeviceID: "d", RequestHash: "h"})
	require.NoError(t, err)
	reopened, err := OpenJournal(dir)
	require.NoError(t, err)
	op, ok := reopened.Get("op-crash-001", "d")
	require.True(t, ok)
	require.Equal(t, taskstate.Failed, op.State)
	require.Equal(t, "unknown", op.Effect)
	_, existing, err := reopened.Begin(Operation{ID: "op-crash-001", Kind: "exec", DeviceID: "d", RequestHash: "h"})
	require.NoError(t, err)
	require.True(t, existing, "an interrupted operation is never re-run automatically")
}

func TestOperationsAreScopedToTheirDevice(t *testing.T) {
	h := newHarness(t, []Capability{CapExec}, nil)
	a, b := h.pair(t), h.pair(t)
	argv, restore := helperArgv(t, "echo")
	defer restore()
	op, err := a.Exec(t.Context(), ExecRequest{OperationID: "op-scoped-01", Argv: argv})
	require.NoError(t, err)
	_, err = b.Operation(t.Context(), op.ID)
	require.Equal(t, "not_found", apiCode(err))
	_, err = b.Exec(t.Context(), ExecRequest{OperationID: "op-scoped-01", Argv: argv})
	require.Equal(t, "operation_conflict", apiCode(err), "another device cannot replay or hijack an operation")
}

func TestFileTransferIntegrityAndBoundaries(t *testing.T) {
	h := newHarness(t, []Capability{CapFiles}, nil)
	c := h.pair(t)
	local := t.TempDir()
	src := filepath.Join(local, "src.bin")
	require.NoError(t, os.WriteFile(src, []byte("payload-123"), 0o644))

	res, err := c.Push(t.Context(), "op-push-0001", src, "in/payload.bin", false, 0)
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(h.root, "in", "payload.bin"))
	require.NoError(t, err)
	require.Equal(t, "payload-123", string(got))

	// Retrying the same transfer is replayed, not rewritten.
	again, err := c.Push(t.Context(), "op-push-0001", src, "in/payload.bin", false, 0)
	require.NoError(t, err)
	require.True(t, again.Replayed)
	require.Equal(t, res.SHA256, again.SHA256)

	_, err = c.Push(t.Context(), "op-push-0002", src, "../escape.bin", false, 0)
	require.Equal(t, "path_traversal", apiCode(err))
	_, err = c.Push(t.Context(), "op-push-0003", src, "/abs.bin", false, 0)
	require.Equal(t, "absolute_path", apiCode(err))

	pulled, err := c.Pull(t.Context(), "in/payload.bin", local, "back.bin", false, 0)
	require.NoError(t, err)
	require.Equal(t, res.SHA256, pulled.SHA256)
	require.False(t, pulled.Replayed)
	// Rerunning the same pull is a replay; different local content is still
	// a conflict without overwrite.
	again, err = c.Pull(t.Context(), "in/payload.bin", local, "back.bin", false, 0)
	require.NoError(t, err)
	require.True(t, again.Replayed)
	require.NoError(t, os.WriteFile(filepath.Join(local, "back.bin"), []byte("changed"), 0o644))
	_, err = c.Pull(t.Context(), "in/payload.bin", local, "back.bin", false, 0)
	require.Equal(t, "exists", transfer.Code(err))
	_, err = c.Pull(t.Context(), "missing.bin", local, "m.bin", false, 0)
	require.Equal(t, "not_found", apiCode(err))
}

type fakeDesktop struct {
	mu     sync.Mutex
	size   computer.Size
	clicks atomic.Int32
	typed  []string
	blank  bool
}

func (f *fakeDesktop) ScreenSize() (computer.Size, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.size, nil
}

func (f *fakeDesktop) Screenshot() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	if !f.blank {
		img.Set(3, 3, color.White)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes(), nil
}
func (f *fakeDesktop) CursorPosition() (computer.Point, error) { return computer.Point{}, nil }
func (f *fakeDesktop) MoveTo(x, y int) error                   { return nil }
func (f *fakeDesktop) Click(x, y int, _ computer.MouseButton) error {
	f.clicks.Add(1)
	return nil
}
func (f *fakeDesktop) DoubleClick(x, y int) error           { return nil }
func (f *fakeDesktop) Drag(x, y, ex, ey int) error          { return nil }
func (f *fakeDesktop) Scroll(dx, dy int) error              { return nil }
func (f *fakeDesktop) TypeText(t string) error              { f.typed = append(f.typed, t); return nil }
func (f *fakeDesktop) KeyPress(string) error                { return nil }
func (f *fakeDesktop) Hotkey(mods []string, k string) error { return nil }

func TestDesktopInputRequiresTheLatestObservationOfTheSameScreen(t *testing.T) {
	desk := &fakeDesktop{size: computer.Size{Width: 800, Height: 600}}
	h := newHarness(t, []Capability{CapDesktop}, desk)
	c := h.pair(t)

	_, err := c.Input(t.Context(), DesktopInput{OperationID: "op-click-001", ObservationID: "obs-guess", Action: "click", X: 10, Y: 10})
	require.Equal(t, "stale_observation", apiCode(err))
	require.Zero(t, desk.clicks.Load())

	obs, err := c.Observe(t.Context(), true)
	require.NoError(t, err)
	require.False(t, obs.Blank)
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-click-002", ObservationID: obs.ObservationID, Action: "click", X: 900, Y: 10})
	require.Equal(t, "out_of_bounds", apiCode(err))

	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-click-003", ObservationID: obs.ObservationID, Action: "click", X: 10, Y: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, desk.clicks.Load())
	// The observation is consumed by pointer input.
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-click-004", ObservationID: obs.ObservationID, Action: "click", X: 10, Y: 10})
	require.Equal(t, "stale_observation", apiCode(err))

	obs, err = c.Observe(t.Context(), false)
	require.NoError(t, err)
	desk.mu.Lock()
	desk.size = computer.Size{Width: 600, Height: 800} // rotated / resized
	desk.mu.Unlock()
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-click-005", ObservationID: obs.ObservationID, Action: "click", X: 10, Y: 10})
	require.Equal(t, "stale_observation", apiCode(err))
	require.EqualValues(t, 1, desk.clicks.Load())

	// A double-submitted click is replayed, not clicked twice.
	desk.mu.Lock()
	desk.size = computer.Size{Width: 800, Height: 600}
	desk.mu.Unlock()
	obs, err = c.Observe(t.Context(), false)
	require.NoError(t, err)
	in := DesktopInput{OperationID: "op-click-006", ObservationID: obs.ObservationID, Action: "click", X: 5, Y: 5}
	_, err = c.Input(t.Context(), in)
	require.NoError(t, err)
	replay, err := c.Input(t.Context(), in)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.EqualValues(t, 2, desk.clicks.Load())
}

func TestBlankCaptureIsReported(t *testing.T) {
	desk := &fakeDesktop{size: computer.Size{Width: 8, Height: 8}, blank: true}
	h := newHarness(t, []Capability{CapDesktop}, desk)
	obs, err := h.pair(t).Observe(t.Context(), false)
	require.NoError(t, err)
	require.True(t, obs.Blank)
}

func TestDesktopUnavailableIsExplicit(t *testing.T) {
	h := newHarness(t, []Capability{CapDesktop}, nil)
	_, err := h.pair(t).Observe(t.Context(), false)
	require.Equal(t, "desktop_unavailable", apiCode(err))
}

func TestPlanRunsIndependentStepsInParallelAndRespectsDependencies(t *testing.T) {
	t.Parallel()
	steps := []PlanStep{
		{ID: "test", Node: "linux", Action: "exec", Argv: []string{"go", "test"}},
		{ID: "build", Node: "local", Action: "exec", Argv: []string{"go", "build"}},
		{ID: "verify", Node: "win", Action: "exec", Argv: []string{"app"}, DependsOn: []string{"test", "build"}},
	}
	require.NoError(t, ValidatePlan(steps))
	var running, peak atomic.Int32
	var order []string
	var mu sync.Mutex
	res := RunPlan(t.Context(), steps, 4, func(ctx context.Context, s PlanStep) StepResult {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		running.Add(-1)
		mu.Lock()
		order = append(order, s.ID)
		mu.Unlock()
		return StepResult{State: taskstate.Completed, NodeID: "id-" + s.Node}
	}, nil)
	require.Equal(t, taskstate.Completed, res.State)
	require.EqualValues(t, 2, peak.Load(), "independent steps must overlap")
	require.Equal(t, "verify", order[2])
	for _, s := range res.Steps {
		require.Equal(t, "id-"+s.Node, s.NodeID, "results keep their source node")
	}
}

func TestPlanCancelsDependentsOfFailedStepAndReportsPartial(t *testing.T) {
	t.Parallel()
	steps := []PlanStep{
		{ID: "a", Node: "n1", Action: "exec", Argv: []string{"x"}},
		{ID: "b", Node: "n2", Action: "exec", Argv: []string{"y"}},
		{ID: "c", Node: "n1", Action: "exec", Argv: []string{"z"}, DependsOn: []string{"b"}},
	}
	res := RunPlan(t.Context(), steps, 2, func(ctx context.Context, s PlanStep) StepResult {
		if s.ID == "b" {
			return StepResult{State: taskstate.Failed, Error: "boom"}
		}
		return StepResult{State: taskstate.Completed}
	}, nil)
	require.Equal(t, taskstate.Failed, res.State)
	require.True(t, res.Partial)
	require.Equal(t, taskstate.Canceled, res.Steps[2].State)
	require.Contains(t, res.Steps[2].Error, "dependency b")
}

func TestPlanCancellationReachesRunningAndPendingSteps(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	steps := []PlanStep{
		{ID: "long", Node: "n1", Action: "exec", Argv: []string{"x"}},
		{ID: "after", Node: "n2", Action: "exec", Argv: []string{"y"}, DependsOn: []string{"long"}},
	}
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	res := RunPlan(ctx, steps, 2, func(ctx context.Context, s PlanStep) StepResult {
		<-ctx.Done()
		return StepResult{State: taskstate.Canceled, Effect: "unknown", Error: "canceled on node"}
	}, nil)
	require.Equal(t, taskstate.Canceled, res.State)
	for _, s := range res.Steps {
		require.Equal(t, taskstate.Canceled, s.State)
	}
}

func TestPlanValidationRejectsCyclesAndBadFields(t *testing.T) {
	t.Parallel()
	require.Error(t, ValidatePlan([]PlanStep{
		{ID: "a", Node: "n", Action: "exec", Argv: []string{"x"}, DependsOn: []string{"b"}},
		{ID: "b", Node: "n", Action: "exec", Argv: []string{"x"}, DependsOn: []string{"a"}},
	}))
	require.Error(t, ValidatePlan([]PlanStep{{ID: "a", Node: "local", Action: "push", LocalPath: "x", RemotePath: "y"}}))
	require.Error(t, ValidatePlan([]PlanStep{{ID: "a", Node: "n", Action: "rm"}}))
	require.Error(t, ValidatePlan([]PlanStep{{ID: "a", Node: "../x", Action: "exec", Argv: []string{"x"}}}))
}

func TestClassifyHost(t *testing.T) {
	t.Parallel()
	require.Equal(t, NetworkLoopback, ClassifyHost("127.0.0.1"))
	require.Equal(t, NetworkLoopback, ClassifyHost("localhost"))
	require.Equal(t, NetworkLAN, ClassifyHost("192.168.1.5"))
	require.Equal(t, NetworkLAN, ClassifyHost("172.20.0.4"))
	require.Equal(t, NetworkLAN, ClassifyHost("devbox"))
	require.Equal(t, NetworkInternet, ClassifyHost("203.0.113.9"))
	require.Equal(t, NetworkInternet, ClassifyHost("node.example.com"))
}

func TestRegistrySealsTokens(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("ATLAS_VAULT_KEY", "")
	}
	r := OpenRegistry(t.TempDir())
	require.Error(t, r.Put(Entry{Name: "local"}, NewToken()), "local is reserved")
	token := NewToken()
	require.NoError(t, r.Put(Entry{Name: "wsl", URL: "https://127.0.0.1:7443", Fingerprint: "sha256:00"}, token))
	data, err := os.ReadFile(r.path)
	require.NoError(t, err)
	if runtime.GOOS == "windows" {
		require.NotContains(t, string(data), token, "the token must not be stored in plaintext")
	}
	_, got, err := r.Get("wsl")
	require.NoError(t, err)
	require.Equal(t, token, got)
	list, err := r.List()
	require.NoError(t, err)
	require.Empty(t, list[0].SealedToken)
	require.NoError(t, r.Remove("wsl"))
	_, _, err = r.Get("wsl")
	require.Error(t, err)
}

// Reported session: click into a prompt box with observation O (consuming
// it), then type with the same O. Keyboard input aims at the focused
// window, not at a point, so the observation the click consumed in this
// session still authorizes it; an unknown or foreign observation does not.
func TestKeyboardInputAfterPointerKeepsTheConsumedObservation(t *testing.T) {
	desk := &fakeDesktop{size: computer.Size{Width: 800, Height: 600}}
	h := newHarness(t, []Capability{CapDesktop}, desk)
	c := h.pair(t)

	obs, err := c.Observe(t.Context(), false)
	require.NoError(t, err)
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-kb-click", ObservationID: obs.ObservationID, Action: "click", X: 10, Y: 10})
	require.NoError(t, err)
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-kb-type", ObservationID: obs.ObservationID, Action: "type", Text: "merhaba"})
	require.NoError(t, err)
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-kb-enter", ObservationID: obs.ObservationID, Action: "key", Key: "enter"})
	require.NoError(t, err)
	desk.mu.Lock()
	require.Equal(t, []string{"merhaba"}, desk.typed)
	desk.mu.Unlock()

	// Pointer input still needs a fresh observation.
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-kb-click2", ObservationID: obs.ObservationID, Action: "click", X: 10, Y: 10})
	require.Equal(t, "stale_observation", apiCode(err))
	// A made-up observation never authorizes keyboard input.
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-kb-type2", ObservationID: "obs-guess", Action: "type", Text: "x"})
	require.Equal(t, "stale_observation", apiCode(err))
	// A newer observation replaces the consumed one.
	_, err = c.Observe(t.Context(), false)
	require.NoError(t, err)
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "op-kb-type3", ObservationID: obs.ObservationID, Action: "type", Text: "y"})
	require.Equal(t, "stale_observation", apiCode(err))
}
