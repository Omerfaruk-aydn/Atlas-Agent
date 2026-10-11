package support

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
	"github.com/stretchr/testify/require"
)

// The test binary doubles as a helper program for remote commands, so
// tests need no platform-specific executables.
func TestMain(m *testing.M) {
	for _, arg := range os.Args[1:] {
		mode, ok := strings.CutPrefix(arg, "-support-helper=")
		if !ok {
			continue
		}
		switch mode {
		case "echo":
			_, _ = os.Stdout.WriteString("hello from the supported computer\n")
		case "mark":
			f, err := os.OpenFile("marks.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err == nil {
				_, _ = f.WriteString("ran\n")
				_ = f.Close()
			}
			time.Sleep(1500 * time.Millisecond)
		case "sleep":
			time.Sleep(time.Minute)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(t *testing.T, mode string) []string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return []string{exe, "-support-helper=" + mode}
}

type fakeDesktop struct {
	mu     sync.Mutex
	size   computer.Size
	clicks atomic.Int32
}

func (f *fakeDesktop) ScreenSize() (computer.Size, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.size, nil
}

func (f *fakeDesktop) Screenshot() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(3, 3, color.White)
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes(), nil
}
func (f *fakeDesktop) CursorPosition() (computer.Point, error) { return computer.Point{}, nil }
func (f *fakeDesktop) MoveTo(int, int) error                   { return nil }
func (f *fakeDesktop) Click(int, int, computer.MouseButton) error {
	f.clicks.Add(1)
	return nil
}
func (f *fakeDesktop) DoubleClick(int, int) error           { return nil }
func (f *fakeDesktop) Drag(int, int, int, int) error        { return nil }
func (f *fakeDesktop) Scroll(int, int) error                { return nil }
func (f *fakeDesktop) TypeText(string) error                { return nil }
func (f *fakeDesktop) KeyPress(string) error                { return nil }
func (f *fakeDesktop) Hotkey(mods []string, k string) error { return nil }

type fakeOverlay struct {
	mu      sync.Mutex
	started int
	ended   int
	open    int
	stop    func()
}

func (o *fakeOverlay) Start(_, _ string, stop func()) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.started++
	o.stop = stop
}

func (o *fakeOverlay) Operation(string, int, int, bool) func() {
	o.mu.Lock()
	o.open++
	o.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			o.mu.Lock()
			o.open--
			o.mu.Unlock()
		})
	}
}

func (o *fakeOverlay) End() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ended++
}

func (o *fakeOverlay) snapshot() (started, ended, open int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.started, o.ended, o.open
}

func startHost(t *testing.T, mut func(*HostOptions)) *Host {
	t.Helper()
	opts := HostOptions{StateDir: t.TempDir(), Name: "owner-pc", LANListen: "127.0.0.1:0", DiscoveryPort: -1, AcceptTimeout: 10 * time.Second, ReconnectWindow: 5 * time.Second}
	if mut != nil {
		mut(&opts)
	}
	h, err := StartHost(context.Background(), opts)
	require.NoError(t, err)
	t.Cleanup(h.Close)
	return h
}

func startController(t *testing.T, mut func(*ControllerOptions)) *Controller {
	t.Helper()
	opts := ControllerOptions{StateDir: t.TempDir(), Name: "helper-pc", DiscoveryPort: -1, ReconnectWindow: 5 * time.Second}
	if mut != nil {
		mut(&opts)
	}
	c, err := NewController(context.Background(), opts)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

// workRoot is an allowed folder on the host, separate from both state
// directories and from the controller's working directory.
func workRoot(t *testing.T) nodes.Root {
	t.Helper()
	return nodes.Root{Label: "Work", Path: t.TempDir()}
}

func offerLAN(t *testing.T, h *Host, caps []Capability, roots ...nodes.Root) OfferInfo {
	t.Helper()
	info, err := h.Offer(OfferOptions{Capabilities: caps, Roots: roots, LAN: true})
	require.NoError(t, err)
	return info
}

func waitPending(t *testing.T, h *Host) *JoinRequest {
	t.Helper()
	var p *JoinRequest
	require.Eventually(t, func() bool {
		p = h.Status().Pending
		return p != nil
	}, 10*time.Second, 10*time.Millisecond)
	return p
}

type joinResult struct {
	remote Remote
	prompt JoinPrompt
	err    error
}

func startJoin(c *Controller, input string) <-chan joinResult {
	ch := make(chan joinResult, 1)
	go func() {
		var prompt JoinPrompt
		r, err := c.Join(context.Background(), input, func(p JoinPrompt) { prompt = p })
		ch <- joinResult{r, prompt, err}
	}()
	return ch
}

func connect(t *testing.T, h *Host, c *Controller, caps []Capability, roots ...nodes.Root) Remote {
	t.Helper()
	info := offerLAN(t, h, caps, roots...)
	ch := startJoin(c, info.Code+"@"+h.LANAddress())
	p := waitPending(t, h)
	require.NoError(t, h.Decide(p.ID, true, nil))
	res := <-ch
	require.NoError(t, res.err)
	require.Equal(t, p.Verification, res.prompt.Verification, "both ends must show the same verification code")
	return res.remote
}

func client(t *testing.T, c *Controller) *nodes.Client {
	t.Helper()
	_, cl, err := c.Target()
	require.NoError(t, err)
	return cl
}

func codeOf(t *testing.T, err error, deviceID string) Code {
	t.Helper()
	require.Error(t, err)
	return Classify(err, deviceID, "").Code
}

// Scenario 1: two independent endpoints pair, and the owner's acceptance
// is what opens the session.
func TestPairingBetweenTwoIndependentEndpoints(t *testing.T) {
	t.Parallel()
	h := startHost(t, nil)
	c := startController(t, nil)
	require.NotEqual(t, h.Fingerprint(), c.Fingerprint())

	root := workRoot(t)
	info := offerLAN(t, h, Presets["diagnose"], root)
	require.Regexp(t, `^[A-Z2-9]{4}-[A-Z2-9]{4}-[A-Z2-9]{4}$`, info.Code)
	require.Contains(t, info.Link, "atlas-support://join?")

	ch := startJoin(c, info.Code+"@"+h.LANAddress())
	p := waitPending(t, h)
	require.Equal(t, "helper-pc", p.Peer.Name)
	require.Equal(t, c.Fingerprint(), p.Peer.Fingerprint, "the owner sees the verified device identity")
	st, _, _ := h.machine.State()
	require.Equal(t, StateAwaitingAcceptance, st)
	// Nothing is reachable before acceptance.
	_, _, err := c.Target()
	require.Equal(t, CodeNoTarget, CodeOf(err))

	require.NoError(t, h.Decide(p.ID, true, nil))
	res := <-ch
	require.NoError(t, res.err)
	r := res.remote
	require.Equal(t, h.NodeID(), r.RemoteDeviceID)
	require.Equal(t, h.Fingerprint(), r.RemoteFingerprint)
	require.Equal(t, uint64(1), r.Generation)
	require.Equal(t, []string{"Work"}, r.Roots)
	require.Equal(t, Presets["diagnose"], r.Capabilities)
	require.Equal(t, p.Verification, res.prompt.Verification)
	require.Equal(t, "lan", r.Network)

	sys, err := client(t, c).SystemInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, h.NodeID(), sys.NodeID)
	require.Equal(t, r.SessionID, sys.SessionID)

	list, err := client(t, c).ListFiles(t.Context(), "", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"Work"}, list.Roots)

	// Histories exist on both computers and hold no code or token.
	for _, hist := range []*History{h.History(), c.History()} {
		events, err := hist.Events(r.SessionID)
		require.NoError(t, err)
		require.NotEmpty(t, events)
		raw, _ := os.ReadFile(filepath.Join(hist.dir, r.SessionID+".jsonl"))
		require.NotContains(t, string(raw), info.Code)
		require.NotContains(t, string(raw), strings.ReplaceAll(info.Code, "-", ""))
	}
}

func TestLANDiscoveryFindsTheHostByCodeAlone(t *testing.T) {
	t.Parallel()
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	port := pc.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, pc.Close())

	h := startHost(t, func(o *HostOptions) { o.DiscoveryPort = port })
	c := startController(t, func(o *ControllerOptions) { o.DiscoveryPort = port; o.DiscoveryWait = 3 * time.Second })
	info := offerLAN(t, h, Presets["diagnose"])
	ch := startJoin(c, strings.ToLower(info.Code))
	p := waitPending(t, h)
	require.NoError(t, h.Decide(p.ID, true, nil))
	res := <-ch
	require.NoError(t, res.err)
	require.Equal(t, h.NodeID(), res.remote.RemoteDeviceID)
}

// Scenario 2: wrong, expired and reused codes.
func TestWrongExpiredAndReusedCodes(t *testing.T) {
	t.Parallel()
	t.Run("wrong code then lockout", func(t *testing.T) {
		t.Parallel()
		h := startHost(t, nil)
		info := offerLAN(t, h, Presets["diagnose"])
		wrong, err := NewPairingCode()
		require.NoError(t, err)
		for i := range maxCodeAttempts {
			c := startController(t, nil)
			_, err := c.Join(t.Context(), wrong.String()+"@"+h.LANAddress(), nil)
			if i < maxCodeAttempts-1 {
				require.Equal(t, CodePairingInvalid, CodeOf(err), "attempt %d", i)
			} else {
				require.Equal(t, CodePairingRateLimited, CodeOf(err))
			}
		}
		// The right code no longer works either: the offer is burned.
		c := startController(t, nil)
		_, err = c.Join(t.Context(), info.Code+"@"+h.LANAddress(), nil)
		require.Error(t, err)
		require.Nil(t, h.Status().Session)
	})
	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		h := startHost(t, nil)
		info, err := h.Offer(OfferOptions{Capabilities: Presets["diagnose"], LAN: true, CodeTTL: 50 * time.Millisecond})
		require.NoError(t, err)
		require.Eventually(t, func() bool { return h.Status().State == StateExpired }, 5*time.Second, 10*time.Millisecond)
		c := startController(t, nil)
		_, err = c.Join(t.Context(), info.Code+"@"+h.LANAddress(), nil)
		require.Equal(t, CodePairingExpired, CodeOf(err))
		require.Equal(t, StateExpired, c.Status().State)
	})
	t.Run("reused", func(t *testing.T) {
		t.Parallel()
		h := startHost(t, nil)
		c := startController(t, nil)
		info := offerLAN(t, h, Presets["diagnose"])
		ch := startJoin(c, info.Code+"@"+h.LANAddress())
		require.NoError(t, h.Decide(waitPending(t, h).ID, true, nil))
		require.NoError(t, (<-ch).err)
		other := startController(t, nil)
		_, err := other.Join(t.Context(), info.Code+"@"+h.LANAddress(), nil)
		require.Equal(t, CodePairingUsed, CodeOf(err))
		require.Equal(t, c.Fingerprint(), h.Status().Session.Peer.Fingerprint, "the first session is untouched")
	})
}

// Scenario 3: a declined request leaves zero remote operations.
func TestDeclinedRequestRunsNothing(t *testing.T) {
	t.Parallel()
	h := startHost(t, nil)
	c := startController(t, nil)
	info := offerLAN(t, h, Presets["full"], workRoot(t))
	ch := startJoin(c, info.Code+"@"+h.LANAddress())
	require.NoError(t, h.Decide(waitPending(t, h).ID, false, nil))
	res := <-ch
	require.Equal(t, CodePairingRejected, CodeOf(res.err))
	_, _, err := c.Target()
	require.Equal(t, CodeNoTarget, CodeOf(err))
	require.Equal(t, StateRevoked, c.Status().State)
	require.Equal(t, StateRevoked, h.Status().State)
	require.Nil(t, h.Status().Session)
	// The code was consumed by the verified request.
	again := startController(t, nil)
	_, err = again.Join(t.Context(), info.Code+"@"+h.LANAddress(), nil)
	require.Error(t, err)
}

// Scenarios 4 and 5: capabilities are enforced on the supported computer,
// whatever the supporter's side decides or sends.
func TestViewOnlySessionRefusesCommandsAndInput(t *testing.T) {
	t.Parallel()
	desk := &fakeDesktop{size: computer.Size{Width: 800, Height: 600}}
	h := startHost(t, func(o *HostOptions) { o.Desktop = desk })
	c := startController(t, nil)
	r := connect(t, h, c, []Capability{CapScreenView})
	cl := client(t, c)

	obs, err := cl.Observe(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, r.SessionID, obs.SessionID)
	require.Equal(t, h.NodeID(), obs.NodeID)
	require.Equal(t, r.Generation, obs.Generation)

	_, err = cl.Input(t.Context(), nodes.DesktopInput{OperationID: "op-click-1", ObservationID: obs.ObservationID, Action: "click", X: 5, Y: 5})
	require.Equal(t, CodeCapabilityDenied, codeOf(t, err, r.RemoteDeviceID))
	require.Zero(t, desk.clicks.Load())

	// A controller that skips its own approvals (auto-approve) talks to the
	// same host check: the raw API refuses an ungranted command.
	_, err = cl.Exec(t.Context(), nodes.ExecRequest{OperationID: "op-exec-1", Argv: helper(t, "echo")})
	require.Equal(t, CodeCapabilityDenied, codeOf(t, err, r.RemoteDeviceID))
	_, err = cl.Launch(t.Context(), "op-launch-1", []string{"notepad"})
	require.Equal(t, CodeCapabilityDenied, codeOf(t, err, r.RemoteDeviceID))

	// An installer needs install on top of command.
	h2 := startHost(t, nil)
	c2 := startController(t, nil)
	r2 := connect(t, h2, c2, []Capability{CapCommand})
	_, err = client(t, c2).Exec(t.Context(), nodes.ExecRequest{OperationID: "op-install", Argv: []string{"winget", "install", "Notepad++.Notepad++"}})
	e := Classify(err, r2.RemoteDeviceID, "op-install")
	require.Equal(t, CodeCapabilityDenied, e.Code)
	require.False(t, e.InputSent)
	require.Equal(t, EffectNone, e.Effect)
}

// Scenario 6: requests naming another session or device are refused.
func TestWrongDeviceOrSessionIsRefused(t *testing.T) {
	t.Parallel()
	h := startHost(t, nil)
	c := startController(t, nil)
	r := connect(t, h, c, Presets["diagnose"])
	c.mu.Lock()
	cc, token := c.cc, c.token
	c.mu.Unlock()
	for name, tr := range map[string]*sessionTransport{
		"device":     {cc: cc, session: r.SessionID, target: "node-someone-else", generation: r.Generation},
		"session":    {cc: cc, session: "ss-0000000000000000", target: r.RemoteDeviceID, generation: r.Generation},
		"generation": {cc: cc, session: r.SessionID, target: r.RemoteDeviceID, generation: r.Generation + 7},
	} {
		cl := &nodes.Client{BaseURL: "https://atlas-support", Token: token, HTTP: &http.Client{Transport: tr}}
		_, err := cl.SystemInfo(t.Context())
		want := CodeWrongTarget
		if name == "generation" {
			want = CodeStaleConnection
		}
		require.Equal(t, want, codeOf(t, err, r.RemoteDeviceID), name)
	}
	// A wrong token is refused too.
	cl := &nodes.Client{BaseURL: "https://atlas-support", Token: "forged", HTTP: &http.Client{Transport: &sessionTransport{cc: cc, session: r.SessionID, target: r.RemoteDeviceID, generation: r.Generation}}}
	_, err := cl.SystemInfo(t.Context())
	require.Equal(t, CodeSessionRevoked, codeOf(t, err, r.RemoteDeviceID))
}

// Scenarios 7, 8 and 9: a dropped connection makes a mutation's outcome
// unknown; nothing re-sends it, the operation id reconciles it, and the
// old connection cannot speak for the new one.
func TestLostConnectionReconcilesWithoutRerunning(t *testing.T) {
	t.Parallel()
	root := workRoot(t)
	h := startHost(t, nil)
	c := startController(t, nil)
	r := connect(t, h, c, []Capability{CapCommand}, root)
	cl := client(t, c)
	c.mu.Lock()
	oldConn, oldCC := c.conn, c.cc
	c.mu.Unlock()

	errc := make(chan error, 1)
	go func() {
		_, err := cl.Exec(context.Background(), nodes.ExecRequest{OperationID: "op-mark-1", Argv: helper(t, "mark"), Cwd: "Work"})
		errc <- err
	}()
	marks := filepath.Join(root.Path, "marks.txt")
	require.Eventually(t, func() bool { _, err := os.Stat(marks); return err == nil }, 10*time.Second, 10*time.Millisecond)
	_ = oldConn.Close()
	e := Classify(<-errc, r.RemoteDeviceID, "op-mark-1")
	require.Equal(t, CodeOutcomeUnknown, e.Code)
	require.True(t, e.InputSent)
	require.Equal(t, EffectUnknown, e.Effect)
	require.False(t, e.Retryable)

	// The controller reconnects under a new generation.
	require.Eventually(t, func() bool {
		st := c.Status()
		return st.State == StateConnected && st.Remote != nil && st.Remote.Generation == r.Generation+1
	}, 10*time.Second, 20*time.Millisecond)
	cl = client(t, c)

	// Reconcile by operation id: it ran once and completes on its own.
	var op nodes.Operation
	require.Eventually(t, func() bool {
		var err error
		op, err = cl.Operation(t.Context(), "op-mark-1")
		return err == nil && op.State.Terminal()
	}, 10*time.Second, 50*time.Millisecond)
	require.Equal(t, taskstate.Completed, op.State)
	require.Equal(t, r.SessionID, op.SessionID)
	require.Equal(t, r.Generation, op.Generation, "the operation stays attributed to the connection that started it")

	// The same id with the same request is a replay; with a different
	// request it is refused.
	again, err := cl.Exec(t.Context(), nodes.ExecRequest{OperationID: "op-mark-1", Argv: helper(t, "mark"), Cwd: "Work"})
	require.NoError(t, err)
	require.True(t, again.Replayed)
	_, err = cl.Exec(t.Context(), nodes.ExecRequest{OperationID: "op-mark-1", Argv: helper(t, "echo"), Cwd: "Work"})
	require.Equal(t, Code("operation_conflict"), codeOf(t, err, r.RemoteDeviceID))
	data, err := os.ReadFile(marks)
	require.NoError(t, err)
	require.Equal(t, "ran\n", string(data), "the command ran exactly once")

	// The old connection is closed and its generation is stale.
	require.False(t, oldCC.CanTakeNewRequest())
	c.mu.Lock()
	cc, token := c.cc, c.token
	c.mu.Unlock()
	stale := &nodes.Client{BaseURL: "https://atlas-support", Token: token, HTTP: &http.Client{Transport: &sessionTransport{cc: cc, session: r.SessionID, target: r.RemoteDeviceID, generation: r.Generation}}}
	_, err = stale.Exec(t.Context(), nodes.ExecRequest{OperationID: "op-mark-2", Argv: helper(t, "mark"), Cwd: "Work"})
	require.Equal(t, CodeStaleConnection, codeOf(t, err, r.RemoteDeviceID))
}

// Scenarios 10 and 19: the owner's disconnect stops new input at once and
// removes every indicator; a later request is refused and reconnecting
// does not regain access.
func TestOwnerDisconnectStopsInputAndRestoresOverlay(t *testing.T) {
	t.Parallel()
	desk := &fakeDesktop{size: computer.Size{Width: 800, Height: 600}}
	overlay := &fakeOverlay{}
	h := startHost(t, func(o *HostOptions) { o.Desktop = desk; o.Overlay = overlay })
	c := startController(t, nil)
	r := connect(t, h, c, []Capability{CapScreenView, CapInput, CapCommand})
	cl := client(t, c)

	errc := make(chan error, 1)
	go func() {
		_, err := cl.Exec(context.Background(), nodes.ExecRequest{OperationID: "op-long-0", Argv: helper(t, "sleep")})
		errc <- err
	}()
	require.Eventually(t, func() bool { _, _, open := overlay.snapshot(); return open == 1 }, 10*time.Second, 10*time.Millisecond)

	h.Disconnect("the owner pressed disconnect")
	require.Equal(t, StateRevoked, h.Status().State)
	started, ended, open := overlay.snapshot()
	require.Equal(t, 1, started)
	require.Equal(t, 1, ended)
	require.Zero(t, open, "operation indicators are cleared")
	require.Error(t, <-errc)

	_, err := cl.Input(t.Context(), nodes.DesktopInput{OperationID: "op-click", ObservationID: "x", Action: "click", X: 1, Y: 1})
	require.Error(t, err)
	require.Zero(t, desk.clicks.Load())

	// The controller learns the session is over and does not get back in.
	require.Eventually(t, func() bool { return c.Status().State.Terminal() }, 15*time.Second, 20*time.Millisecond)
	_, _, err = c.Target()
	require.Error(t, err)
	require.Equal(t, StateRevoked, c.Status().State)

	// The stopped command is recorded honestly on the owner's side.
	events, err := h.History().Events(r.SessionID)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(events, func(e Event) bool {
		return e.Kind == "operation_stopped" && e.OperationID == "op-long-0" && e.Effect == "unknown"
	}), "%+v", events)
}

func TestBannerStopEndsTheSession(t *testing.T) {
	t.Parallel()
	overlay := &fakeOverlay{}
	h := startHost(t, func(o *HostOptions) { o.Overlay = overlay })
	c := startController(t, nil)
	connect(t, h, c, Presets["diagnose"])
	overlay.mu.Lock()
	stop := overlay.stop
	overlay.mu.Unlock()
	stop()
	require.Equal(t, StateRevoked, h.Status().State)
	_, ended, _ := overlay.snapshot()
	require.Equal(t, 1, ended)
}

func TestLostSupporterEndsSessionAfterReconnectWindow(t *testing.T) {
	t.Parallel()
	overlay := &fakeOverlay{}
	h := startHost(t, func(o *HostOptions) { o.Overlay = overlay; o.ReconnectWindow = 300 * time.Millisecond })
	c := startController(t, nil)
	connect(t, h, c, Presets["diagnose"])
	c.Close()
	require.Eventually(t, func() bool { return h.Status().State == StateDisconnected }, 10*time.Second, 20*time.Millisecond)
	_, ended, _ := overlay.snapshot()
	require.Equal(t, 1, ended)
}

// Scenarios 11, 12 and 13: chunked transfers verify hashes, stop when the
// owner disconnects, and never leave the allowed folders.
func TestTransfersVerifyStopAndStayInside(t *testing.T) {
	t.Parallel()
	root := workRoot(t)
	h := startHost(t, nil)
	c := startController(t, nil)
	r := connect(t, h, c, []Capability{CapFileRead, CapFileSend, CapFileReceive}, root)
	cl := client(t, c)
	local := t.TempDir()

	src := filepath.Join(local, "report.txt")
	require.NoError(t, os.WriteFile(src, []byte("quarterly numbers"), 0o600))
	up, err := cl.Upload(t.Context(), "op-up-1-0", src, "Work/in/report.txt", transfer.ConflictError, 0, nil)
	require.NoError(t, err)
	require.Equal(t, "complete", up.State)
	require.Equal(t, "Work/in/report.txt", up.Path)
	got, err := os.ReadFile(filepath.Join(root.Path, "in", "report.txt"))
	require.NoError(t, err)
	require.Equal(t, "quarterly numbers", string(got))

	// Same name, rename policy: a new name, nothing overwritten.
	require.NoError(t, os.WriteFile(src, []byte("other numbers"), 0o600))
	renamed, err := cl.Upload(t.Context(), "op-up-2-0", src, "Work/in/report.txt", transfer.ConflictRename, 0, nil)
	require.NoError(t, err)
	require.Equal(t, "Work/in/report (1).txt", renamed.Path)
	// Overwrite needs file_modify, which was not granted.
	_, err = cl.Upload(t.Context(), "op-up-3-0", src, "Work/in/report.txt", transfer.ConflictOverwrite, 0, nil)
	require.Equal(t, CodeCapabilityDenied, codeOf(t, err, r.RemoteDeviceID))

	// Download back with hash verification.
	pulled, err := cl.Pull(t.Context(), "Work/in/report.txt", local, "copy.txt", false, 0)
	require.NoError(t, err)
	sum := sha256.Sum256([]byte("quarterly numbers"))
	require.Equal(t, hex.EncodeToString(sum[:]), pulled.SHA256)

	// A declared hash that does not match the bytes fails and delivers
	// nothing.
	var bad nodes.UploadResult
	_, err = c.supportCall(t.Context(), http.MethodPost, "/v1/node/uploads", nodes.UploadRequest{OperationID: "op-bad-00", Path: "Work/bad.txt", Size: 3, SHA256: strings.Repeat("0", 64)}, &bad)
	require.NoError(t, err)
	err = putChunk(t, c, bad.ID, 0, []byte("abc"))
	require.NoError(t, err)
	_, err = c.supportCall(t.Context(), http.MethodPost, "/v1/node/uploads/"+bad.ID+"/complete", struct{}{}, nil)
	require.Equal(t, CodeHashMismatch, CodeOf(err))
	require.NoFileExists(t, filepath.Join(root.Path, "bad.txt"))

	// Traversal and unknown folders are refused.
	for i, rel := range []string{"Work/../escape.txt", "Other/x.txt", "../x.txt", "Work/..\\escape.txt"} {
		_, err := cl.Upload(t.Context(), "op-escape-"+strconv.Itoa(i), src, rel, transfer.ConflictError, 0, nil)
		require.Equal(t, CodePathDenied, codeOf(t, err, r.RemoteDeviceID), rel)
	}
	_, err = cl.Pull(t.Context(), "Work/../../secret", local, "s.txt", false, 0)
	require.Equal(t, CodePathDenied, codeOf(t, err, r.RemoteDeviceID))
	_, err = os.Stat(filepath.Join(filepath.Dir(root.Path), "escape.txt"))
	require.True(t, errors.Is(err, os.ErrNotExist))

	// A large upload stops promptly when the owner disconnects, and the
	// partial file is never delivered.
	big := filepath.Join(local, "big.bin")
	data := bytes.Repeat([]byte("0123456789abcdef"), int((3*transfer.MaxChunkBytes)/16+7))
	require.NoError(t, os.WriteFile(big, data, 0o600))
	var once sync.Once
	start := time.Now()
	_, err = cl.Upload(t.Context(), "op-big-00", big, "Work/big.bin", transfer.ConflictError, 0, func(sent, total int64) {
		once.Do(func() { h.Disconnect("stop the transfer") })
	})
	require.Error(t, err)
	require.Less(t, time.Since(start), 20*time.Second)
	require.NoFileExists(t, filepath.Join(root.Path, "big.bin"))
}

func putChunk(t *testing.T, c *Controller, id string, offset int64, data []byte) error {
	t.Helper()
	_, cl, err := c.Target()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPut, cl.BaseURL+"/v1/node/uploads/"+id+"?offset="+strconv.FormatInt(offset, 10), bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+cl.Token)
	req.Header.Set("X-Atlas-Chunk-SHA256", hex.EncodeToString(sum[:]))
	resp, err := cl.HTTP.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}

// Scenario 14: permission answers bind to the request id, revision and
// session they were shown for.
func TestPermissionAnswersBindToTheRequest(t *testing.T) {
	t.Parallel()
	h := startHost(t, nil)
	c := startController(t, nil)
	connect(t, h, c, []Capability{CapSystemInfo})

	first, err := c.RequestPermission(t.Context(), []Capability{CapCommand}, "check the event log")
	require.NoError(t, err)
	require.Equal(t, PermissionPending, first.State)
	require.Equal(t, first.ID, h.Status().Permission.ID)

	// A newer request supersedes it; an answer to the old one is stale.
	second, err := c.RequestPermission(t.Context(), []Capability{CapCommand, CapInstall}, "install the printer driver")
	require.NoError(t, err)
	require.ErrorIs(t, h.AnswerPermission(first.ID, first.Revision, true), ErrStaleAnswer)
	require.ErrorIs(t, h.AnswerPermission(second.ID, second.Revision-1, true), ErrStaleAnswer)

	// A canceled request cannot be answered back to life.
	_, err = c.CancelPermission(t.Context(), second.ID)
	require.NoError(t, err)
	require.ErrorIs(t, h.AnswerPermission(second.ID, second.Revision, true), ErrStaleAnswer)
	r, err := c.Refresh(t.Context())
	require.NoError(t, err)
	require.Equal(t, []Capability{CapSystemInfo}, r.Capabilities)

	third, err := c.RequestPermission(t.Context(), []Capability{CapCommand}, "check the event log")
	require.NoError(t, err)
	require.NoError(t, h.AnswerPermission(third.ID, third.Revision, true))
	status, err := c.PermissionStatus(t.Context(), third.ID)
	require.NoError(t, err)
	require.Equal(t, PermissionGranted, status.State)
	r = *c.Status().Remote
	require.Contains(t, r.Capabilities, CapCommand)
	require.NotContains(t, r.Capabilities, CapInstall, "only what was asked and answered is granted")
}

// Scenario 15: an unavailable remote desktop is reported as such.
func TestRemoteDesktopUnavailableIsExplicit(t *testing.T) {
	t.Parallel()
	h := startHost(t, func(o *HostOptions) { o.DesktopReason = "no interactive session" })
	c := startController(t, nil)
	r := connect(t, h, c, []Capability{CapScreenView, CapInput})
	require.False(t, r.Desktop)
	_, err := client(t, c).Observe(t.Context(), false)
	e := Classify(err, r.RemoteDeviceID, "")
	require.Equal(t, CodeDesktopUnavailable, e.Code)
	require.Equal(t, r.RemoteDeviceID, e.RemoteDeviceID)
}

func TestOwnerPausesAreSeparate(t *testing.T) {
	t.Parallel()
	desk := &fakeDesktop{size: computer.Size{Width: 800, Height: 600}}
	h := startHost(t, func(o *HostOptions) { o.Desktop = desk })
	c := startController(t, nil)
	r := connect(t, h, c, []Capability{CapSystemInfo, CapCommand, CapScreenView, CapInput})
	cl := client(t, c)

	require.NoError(t, h.PauseInput(true))
	obs, err := cl.Observe(t.Context(), false)
	require.NoError(t, err, "viewing continues while screen control is paused")
	_, err = cl.Input(t.Context(), nodes.DesktopInput{OperationID: "op-c1-000", ObservationID: obs.ObservationID, Action: "click", X: 1, Y: 1})
	require.Equal(t, CodeControlPaused, codeOf(t, err, r.RemoteDeviceID))
	_, err = cl.Exec(t.Context(), nodes.ExecRequest{OperationID: "op-e1-000", Argv: helper(t, "echo")})
	require.NoError(t, err, "commands are not screen control")
	require.NoError(t, h.PauseInput(false))

	require.NoError(t, h.PauseAgent(true))
	require.Equal(t, StatePaused, h.Status().State)
	_, err = cl.Exec(t.Context(), nodes.ExecRequest{OperationID: "op-e2-000", Argv: helper(t, "echo")})
	require.Equal(t, CodeSessionPaused, codeOf(t, err, r.RemoteDeviceID))
	_, err = cl.SystemInfo(t.Context())
	require.NoError(t, err, "reading stays available while the task is paused")
	ref, err := c.Refresh(t.Context())
	require.NoError(t, err)
	require.True(t, ref.AgentPaused)
	require.NoError(t, h.PauseAgent(false))
	_, err = cl.Exec(t.Context(), nodes.ExecRequest{OperationID: "op-e3-000", Argv: helper(t, "echo")})
	require.NoError(t, err)

	require.NoError(t, h.SetCapabilities([]Capability{CapSystemInfo}))
	_, err = cl.Exec(t.Context(), nodes.ExecRequest{OperationID: "op-e4-000", Argv: helper(t, "echo")})
	require.Equal(t, CodeCapabilityDenied, codeOf(t, err, r.RemoteDeviceID))
	require.Zero(t, desk.clicks.Load())
}

// Scenario 20: after a restart, an ended session stays ended.
func TestRestartDoesNotReviveSessions(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	h1 := startHost(t, func(o *HostOptions) { o.StateDir = state })
	c := startController(t, nil)
	r := connect(t, h1, c, Presets["diagnose"])
	// An interrupted record (as after a crash) is closed on start.
	store := sessionStore{dir: filepath.Join(state, "sessions")}
	require.NoError(t, store.save(sessionRecord{ID: "ss-crashed00000000", State: StateConnected}))

	addr := h1.LANAddress()
	h1.Close()
	rec, ok := store.load(r.SessionID)
	require.True(t, ok)
	require.Equal(t, StateDisconnected, rec.State)

	h2 := startHost(t, func(o *HostOptions) { o.StateDir = state; o.LANListen = addr })
	require.Equal(t, h1.NodeID(), h2.NodeID(), "same computer, same identity")
	crashed, ok := store.load("ss-crashed00000000")
	require.True(t, ok)
	require.Equal(t, StateDisconnected, crashed.State)
	_, err := h2.Offer(OfferOptions{Capabilities: Presets["diagnose"], LAN: true})
	require.NoError(t, err)

	err = c.resumeOnce(Remote{SessionInfo: r.SessionInfo, Address: addr}, r.Generation)
	require.Equal(t, CodeSessionRevoked, CodeOf(err))
	require.Nil(t, h2.Status().Session)
}

// Internet mode goes only through a configured relay, and fails clearly
// without one.
func TestRelayModeAndItsAbsence(t *testing.T) {
	t.Parallel()
	t.Run("not configured", func(t *testing.T) {
		t.Parallel()
		h := startHost(t, nil)
		_, err := h.Offer(OfferOptions{Capabilities: Presets["diagnose"], Internet: true})
		require.Equal(t, CodeRelayNotConfigured, CodeOf(err))
		require.Equal(t, StateIdle, h.Status().State, "no fake hosting state")

		code, _ := NewPairingCode()
		c := startController(t, nil)
		_, err = c.Join(t.Context(), code.String(), nil)
		require.Equal(t, CodeRemoteOffline, CodeOf(err))
		require.Contains(t, err.Error(), "support_relay_url")
		require.Equal(t, StateFailed, c.Status().State)

		link := JoinLink(code, "https://relay.example.invalid", "")
		_, err = c.Join(t.Context(), link, nil)
		require.Equal(t, CodeRelayNotConfigured, CodeOf(err), "a link cannot make Atlas contact an unconfigured relay")
	})
	t.Run("through relay with reconnect", func(t *testing.T) {
		t.Parallel()
		var logs bytes.Buffer
		relay := NewRelayServer(RelayServerOptions{Token: "relay-secret", Logger: slogTo(&logs)})
		srv := httptest.NewServer(relay)
		t.Cleanup(srv.Close)
		cfg := &RelayConfig{URL: srv.URL, Token: "relay-secret"}

		h := startHost(t, func(o *HostOptions) { o.Relay = cfg })
		c := startController(t, func(o *ControllerOptions) { o.Relay = cfg })
		info, err := h.Offer(OfferOptions{Capabilities: Presets["diagnose"], Internet: true})
		require.NoError(t, err)
		require.Empty(t, info.LANAddresses)
		require.Eventually(t, func() bool { return relay.Waiting() == 1 }, 10*time.Second, 10*time.Millisecond)

		ch := startJoin(c, info.Link)
		p := waitPending(t, h)
		require.Equal(t, "relay", p.Network)
		require.NoError(t, h.Decide(p.ID, true, nil))
		res := <-ch
		require.NoError(t, res.err)
		require.Equal(t, "relay", res.remote.Network)
		_, err = client(t, c).SystemInfo(t.Context())
		require.NoError(t, err)

		// Reconnect through the relay's resume slot.
		require.Eventually(t, func() bool { return relay.Waiting() == 1 }, 10*time.Second, 10*time.Millisecond)
		c.mu.Lock()
		_ = c.conn.Close()
		c.mu.Unlock()
		require.Eventually(t, func() bool {
			st := c.Status()
			return st.State == StateConnected && st.Remote.Generation == 2
		}, 15*time.Second, 20*time.Millisecond)
		_, err = client(t, c).SystemInfo(t.Context())
		require.NoError(t, err)

		require.NotContains(t, logs.String(), info.Code)
		require.NotContains(t, logs.String(), res.remote.SessionID)

		// Wrong relay token: a configuration error, not a fake connection.
		bad := startController(t, func(o *ControllerOptions) { o.Relay = &RelayConfig{URL: srv.URL, Token: "wrong"} })
		code, _ := NewPairingCode()
		_, err = bad.Join(t.Context(), code.String(), nil)
		require.Equal(t, CodeRelayNotConfigured, CodeOf(err))
	})
}

