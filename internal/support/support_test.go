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

