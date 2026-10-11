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

