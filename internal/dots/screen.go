package dots

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
)

// Screen dimensions of a dot's private browser, in CSS pixels.
const (
	ScreenWidth  = 1280
	ScreenHeight = 800
)

// ScreenNote is the plain description shown next to the live picture.
const ScreenNote = "Bu ekran senin masaüstün değil. Bu bilgisayarda donanım sanallaştırması kapalı olduğu için dot, kendi Chrome profilinde çalışır."

// IsolationChrome is the environment kind actually running.
const IsolationChrome = "chrome_profile"

// ErrHeld means the owner is driving the screen, so the dot must not.
var ErrHeld = errors.New("the owner is driving this screen; nothing was sent")

// ErrScreenOff means the private browser is not running.
var ErrScreenOff = errors.New("the dot's screen is not running")

// Endpoint is the loopback DevTools address of one dot's browser.
type Endpoint struct {
	HTTP      string    `json:"http"`
	PID       int       `json:"pid"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	TakenOver bool      `json:"taken_over"`
	StartedAt time.Time `json:"started_at"`
}

// ScreenStatus is what the screen tab reads.
type ScreenStatus struct {
	Running   bool   `json:"running"`
	Starting  bool   `json:"starting,omitempty"`
	TakenOver bool   `json:"taken_over"`
	URL       string `json:"url,omitempty"`
	Title     string `json:"title,omitempty"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Isolation string `json:"isolation"`
	Note      string `json:"note"`
}

// Pointer is one owner or agent input event on the private browser.
type Pointer struct {
	Kind   string
	X      int
	Y      int
	DX     int
	DY     int
	Text   string
	Key    string
	Mods   []string
	Button string
}

type pageTarget struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
	WS    string `json:"webSocketDebuggerUrl"`
}

var (
	screenMu sync.Mutex
	screenOf = map[string]*sync.Mutex{}
)

func lockScreen(workspace string) func() {
	screenMu.Lock()
	m := screenOf[workspace]
	if m == nil {
		m = &sync.Mutex{}
		screenOf[workspace] = m
	}
	screenMu.Unlock()
	m.Lock()
	return m.Unlock
}

func screenDir(workspace string) string {
	return filepath.Join(workspace, "screen")
}

// ProfileDir is the Chrome user-data directory for a dot. It is never the
// owner's Chrome profile.
func ProfileDir(workspace string) string {
	return filepath.Join(screenDir(workspace), "profile")
}

func endpointPath(workspace string) string {
	return filepath.Join(screenDir(workspace), "endpoint.json")
}

func readEndpoint(workspace string) (Endpoint, error) {
	raw, err := os.ReadFile(endpointPath(workspace))
	if err != nil {
		return Endpoint{}, err
	}
	var ep Endpoint
	if err := json.Unmarshal(raw, &ep); err != nil {
		return Endpoint{}, err
	}
	return ep, nil
}

func writeEndpoint(workspace string, ep Endpoint) error {
	if err := os.MkdirAll(screenDir(workspace), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(ep, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(endpointPath(workspace), raw)
}

// ErrIfHeld reports ErrHeld when the owner has taken the screen.
func ErrIfHeld(workspace string) error {
	ep, err := readEndpoint(workspace)
	if err != nil || !ep.TakenOver {
		return nil
	}
	if !endpointAlive(ep) {
		return nil
	}
	return ErrHeld
}

func endpointAlive(ep Endpoint) bool {
	if ep.HTTP == "" || !processAlive(ep.PID) {
		return false
	}
	return devtoolsAlive(ep.HTTP)
}

func devtoolsAlive(base string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/json/version", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Status describes the screen without starting it.
func Status(ctx context.Context, workspace string) ScreenStatus {
	if alive, starting, errText := sandboxState(); alive || starting || errText != "" {
		out := ScreenStatus{Width: ScreenWidth, Height: ScreenHeight, Isolation: IsolationSandbox, Note: SandboxNote, Running: alive, Title: "Ajanın Windows'u"}
		if starting && !alive {
			out.Starting = true
			out.Note = SandboxStarting
		}
		if errText != "" && !alive && !starting {
			out.Note = errText
		}
		return out
	}
	out := ScreenStatus{Width: ScreenWidth, Height: ScreenHeight, Isolation: IsolationChrome, Note: ScreenNote}
	ep, err := readEndpoint(workspace)
	if err != nil || !endpointAlive(ep) {
		return out
	}
	out.Running = true
	out.TakenOver = ep.TakenOver
	out.Width, out.Height = ep.Width, ep.Height
	if page, err := activePage(ctx, ep.HTTP); err == nil {
		out.URL, out.Title = page.URL, page.Title
	}
	return out
}

// Ensure starts the dot's private browser when it is not already up.
func Ensure(ctx context.Context, workspace string) (Endpoint, error) {
	unlock := lockScreen(workspace)
	defer unlock()
	if ep, err := readEndpoint(workspace); err == nil && endpointAlive(ep) {
		return ep, nil
	}
	return launch(ctx, workspace)
}

func launch(ctx context.Context, workspace string) (Endpoint, error) {
	exe := browser.FindChrome()
	if exe == "" {
		return Endpoint{}, errors.New("no Chrome or Chromium found for the dot's screen")
	}
	var cfg net.ListenConfig
	ln, err := cfg.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return Endpoint{}, fmt.Errorf("choose a debugging port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	profile := ProfileDir(workspace)
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return Endpoint{}, err
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-extensions",
		"--disable-background-networking",
		"--force-device-scale-factor=1",
		"--window-size=" + fmt.Sprintf("%d,%d", ScreenWidth, ScreenHeight),
		"--remote-debugging-port=" + fmt.Sprint(port),
		"--remote-debugging-address=127.0.0.1",
		"--remote-allow-origins=*",
		"--user-data-dir=" + profile,
		"about:blank",
	}
	cmd := exec.CommandContext(context.WithoutCancel(ctx), exe, args...)
	hideWindow(cmd)
	logPath := filepath.Join(screenDir(workspace), "chrome.log")
	if logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600); err == nil {
		cmd.Stderr = logf
		defer logf.Close()
	}
	if err := cmd.Start(); err != nil {
		return Endpoint{}, fmt.Errorf("start the dot's browser: %w", err)
	}
	go func() { _ = cmd.Wait() }()

	ep := Endpoint{HTTP: base, PID: cmd.Process.Pid, Width: ScreenWidth, Height: ScreenHeight, StartedAt: time.Now()}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			_ = killTree(ep.PID)
			return Endpoint{}, ctx.Err()
		}
		if devtoolsAlive(base) {
			if err := writeEndpoint(workspace, ep); err != nil {
				_ = killTree(ep.PID)
				return Endpoint{}, err
			}
			slog.Info("Started dot screen", "port", port)
			return ep, nil
		}
		if !processAlive(ep.PID) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = killTree(ep.PID)
	return Endpoint{}, fmt.Errorf("the dot's browser did not open its debugging port; see %s", logPath)
}

// Stop ends the browser this screen started. A browser that is already
// gone is not an error.
func Stop(workspace string) error {
	unlock := lockScreen(workspace)
	defer unlock()
	ep, err := readEndpoint(workspace)
	if err != nil {
		return nil
	}
	if processAlive(ep.PID) {
		if err := killTree(ep.PID); err != nil {
			slog.Warn("Failed to stop dot screen", "error", err)
		}
	}
	ep.PID = 0
	ep.TakenOver = false
	return writeEndpoint(workspace, ep)
}

// StopScreens stops every dot screen this store knows about.
func (s *Store) StopScreens() {
	for _, d := range s.List() {
		if err := Stop(d.Workspace); err != nil {
			slog.Warn("Failed to stop dot screen", "dot", d.ID, "error", err)
		}
	}
}

// SetTakenOver records whether the owner is driving the screen.
func SetTakenOver(workspace string, on bool) (Endpoint, error) {
	unlock := lockScreen(workspace)
	defer unlock()
	ep, err := readEndpoint(workspace)
	if err != nil || !endpointAlive(ep) {
		return Endpoint{}, ErrScreenOff
	}
	ep.TakenOver = on
	if err := writeEndpoint(workspace, ep); err != nil {
		return Endpoint{}, err
	}
	return ep, nil
}

func ensurePage(ctx context.Context, base string) (pageTarget, error) {
	if page, err := activePage(ctx, base); err == nil {
		return page, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, base+"/json/new?"+url.QueryEscape("about:blank"), nil)
	if err != nil {
		return pageTarget{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return pageTarget{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return pageTarget{}, fmt.Errorf("opening a page returned %d", resp.StatusCode)
	}
	var created pageTarget
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&created); err != nil {
		return pageTarget{}, err
	}
	if created.ID == "" {
		return pageTarget{}, errors.New("the dot's browser did not open a page")
	}
	return created, nil
}

func activePage(ctx context.Context, base string) (pageTarget, error) {
	var pages []pageTarget
	if err := getJSON(ctx, base+"/json/list", &pages); err != nil {
		return pageTarget{}, err
	}
	var found pageTarget
	for _, p := range pages {
		if p.Type != "page" || p.ID == "" {
			continue
		}
		if strings.HasPrefix(p.URL, "chrome://") || strings.HasPrefix(p.URL, "chrome-extension://") || strings.HasPrefix(p.URL, "devtools://") {
			continue
		}
		found = p
	}
	if found.ID == "" {
		return pageTarget{}, errors.New("the dot's browser has no page")
	}
	return found, nil
}

func getJSON(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("devtools %s returned %d", rawURL, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dest)
}
