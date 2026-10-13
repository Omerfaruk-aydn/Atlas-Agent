// Package fakecli is a scripted stand-in for the Tailscale CLI and
// tailscaled, for tests only. It models what Atlas relies on: status
// JSON, the Serve configuration with foreground sessions that disappear
// when their CLI process ends, port conflicts, and the browser approval
// and login flows. It never contacts a real tailnet.
package fakecli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/tsfunnel"
)

// FunnelMode scripts how "tailscale funnel" behaves.
type FunnelMode int

// Funnel behaviours.
const (
	// FunnelOK configures the entry at once.
	FunnelOK FunnelMode = iota
	// FunnelApproval prints an approval URL and waits for Approve.
	FunnelApproval
	// FunnelApprovalExit prints an approval URL and exits 0, like a CLI
	// that does not wait for enablement.
	FunnelApprovalExit
	// FunnelHang never configures anything.
	FunnelHang
	// FunnelFail exits 1 with an error.
	FunnelFail
)

// ApprovalURL is the URL the fake prints for enablement.
const ApprovalURL = "https://login.tailscale.com/f/funnel?node=nFAKE"

// LoginURL is the URL the fake prints for sign-in.
const LoginURL = "https://login.tailscale.com/a/fake123"

// Daemon is the fake CLI and daemon. Exported fields may be changed
// between calls while holding no lock; methods lock.
type Daemon struct {
	mu sync.Mutex

	Installed   bool
	ServiceDown bool
	Backend     string
	Version     string
	Host        string
	MagicDNS    bool
	Caps        []string
	// StatusRaw and ServeRaw replace the generated JSON when set.
	StatusRaw []byte
	ServeRaw  []byte
	// Delay delays every short command; it honours cancellation.
	Delay time.Duration
	// Funnel scripts the funnel command.
	Funnel FunnelMode
	// KeepSessions leaves foreground sessions after their CLI stops,
	// as a misbehaving daemon would.
	KeepSessions bool
	// LoginFails makes "tailscale up" fail with this text.
	LoginFails string

	background map[string]any
	foreground map[string]map[string]any
	nextID     int
	calls      [][]string
	approve    chan struct{}
	login      chan struct{}
	procs      []*Proc
}

// New returns a running, logged-in daemon with Funnel allowed.
func New() *Daemon {
	return &Daemon{
		Installed: true, Backend: "Running", Version: "1.104.1-tfake-gfake",
		Host: "atlas-pc.tail1234.ts.net.", MagicDNS: true,
		Caps:       []string{"https", "funnel", "https://tailscale.com/cap/funnel-ports?ports=443,8443,10000"},
		background: map[string]any{}, foreground: map[string]map[string]any{},
		approve: make(chan struct{}), login: make(chan struct{}),
	}
}

var _ tsfunnel.Exec = (*Daemon)(nil)

// SetBackground replaces the background Serve configuration, which
// belongs to other applications.
func (d *Daemon) SetBackground(cfg map[string]any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.background = cfg
}

// AddForeign adds a foreground session of another CLI on port.
func (d *Daemon) AddForeign(id string, port uint16, target string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.foreground[id] = entry(strings.TrimSuffix(d.Host, "."), port, target)
}

// ServeJSON returns the configuration as "funnel status --json" prints it.
func (d *Daemon) ServeJSON() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.serveJSONLocked()
}

// Background returns the current background configuration as JSON.
func (d *Daemon) Background() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, _ := json.Marshal(d.background)
	return b
}

// Sessions lists foreground session ids.
func (d *Daemon) Sessions() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Sorted(maps.Keys(d.foreground))
}

// Calls returns every command run so far.
func (d *Daemon) Calls() [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.calls)
}

// Approve completes a pending Funnel approval.
func (d *Daemon) Approve() { close(d.approve) }

// CompleteLogin finishes a pending "tailscale up".
func (d *Daemon) CompleteLogin() { close(d.login) }

// Path implements tsfunnel.Exec.
func (d *Daemon) Path() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.Installed {
		return "", tsfunnel.ErrNotInstalled
	}
	return `C:\Program Files\Tailscale\tailscale.exe`, nil
}

// Output implements tsfunnel.Exec.
func (d *Daemon) Output(ctx context.Context, args ...string) ([]byte, error) {
	d.mu.Lock()
	d.calls = append(d.calls, slices.Clone(args))
	delay := d.Delay
	d.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, tsfunnel.ErrTimeout
			}
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.Installed {
		return nil, tsfunnel.ErrNotInstalled
	}
	if d.ServiceDown {
		return nil, fmt.Errorf("%w: failed to connect to local Tailscale daemon", tsfunnel.ErrServiceUnavailable)
	}
	switch strings.Join(args, " ") {
	case "status --json --peers=false":
		if d.StatusRaw != nil {
			return d.StatusRaw, nil
		}
		return d.statusJSONLocked(), nil
	case "funnel status --json":
		return d.serveJSONLocked(), nil
	}
	return nil, fmt.Errorf("fake tailscale: unexpected command %q", args)
}

func (d *Daemon) statusJSONLocked() []byte {
	caps := map[string]any{}
	for _, c := range d.Caps {
		caps[c] = nil
	}
	st := map[string]any{
		"Version": d.Version, "BackendState": d.Backend, "AuthURL": "",
		"Self":           map[string]any{"DNSName": d.Host, "Online": true, "CapMap": caps},
		"CurrentTailnet": map[string]any{"Name": "user@example.com", "MagicDNSSuffix": "tail1234.ts.net", "MagicDNSEnabled": d.MagicDNS},
	}
	if d.Backend == "NeedsLogin" {
		st["Self"] = map[string]any{"DNSName": ""}
		st["CurrentTailnet"] = nil
	}
	b, _ := json.Marshal(st)
	return b
}

func (d *Daemon) serveJSONLocked() []byte {
	if d.ServeRaw != nil {
		return d.ServeRaw
	}
	if len(d.background) == 0 && len(d.foreground) == 0 {
		return []byte("null\n")
	}
	cfg := maps.Clone(d.background)
	if len(d.foreground) > 0 {
		fg := map[string]any{}
		for k, v := range d.foreground {
			fg[k] = v
		}
		cfg["Foreground"] = fg
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return b
}

func entry(host string, port uint16, target string) map[string]any {
	hp := net.JoinHostPort(host, strconv.Itoa(int(port)))
	return map[string]any{
		"TCP":         map[string]any{strconv.Itoa(int(port)): map[string]any{"HTTPS": true}},
		"Web":         map[string]any{hp: map[string]any{"Handlers": map[string]any{"/": map[string]any{"Proxy": target}}}},
		"AllowFunnel": map[string]any{hp: true},
	}
}

// portUsedLocked mirrors tailscaled: a new foreground listener must use
// a port no background or foreground config uses.
func (d *Daemon) portUsedLocked(port string) bool {
	used := func(cfg map[string]any) bool {
		tcp, _ := cfg["TCP"].(map[string]any)
		_, ok := tcp[port]
		return ok
	}
	if used(d.background) {
		return true
	}
	for _, fg := range d.foreground {
		if used(fg) {
			return true
		}
	}
	return false
}

// Proc is a fake long-lived CLI process.
type Proc struct {
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	code   int
	Args   []string
	onStop func()
}

func newProc(args []string) *Proc { return &Proc{done: make(chan struct{}), code: -1, Args: args} }

// Identity implements tsfunnel.Process; fakes never match a real process.
func (p *Proc) Identity() procown.Identity { return procown.Identity{} }

// Done implements tsfunnel.Process.
func (p *Proc) Done() <-chan struct{} { return p.done }

// Result implements tsfunnel.Process.
func (p *Proc) Result() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code, nil
}

// Stop implements tsfunnel.Process.
func (p *Proc) Stop(time.Duration) { p.exit(1) }

// Crash ends the process as if it died on its own.
func (p *Proc) Crash() { p.exit(2) }

func (p *Proc) exit(code int) {
	p.once.Do(func() {
		p.mu.Lock()
		p.code = code
		stop := p.onStop
		p.mu.Unlock()
		if stop != nil {
			stop()
		}
		close(p.done)
	})
}

// Procs returns the started processes.
func (d *Daemon) Procs() []*Proc {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.procs)
}

// Start implements tsfunnel.Exec.
func (d *Daemon) Start(args []string, onLine func(string)) (tsfunnel.Process, error) {
	d.mu.Lock()
	d.calls = append(d.calls, slices.Clone(args))
	if !d.Installed {
		d.mu.Unlock()
		return nil, tsfunnel.ErrNotInstalled
	}
	p := newProc(slices.Clone(args))
	d.procs = append(d.procs, p)
	d.mu.Unlock()
	switch {
	case len(args) == 1 && args[0] == "up":
		go d.runUp(p, onLine)
	case len(args) == 3 && args[0] == "funnel" && strings.HasPrefix(args[1], "--https="):
		go d.runFunnel(p, strings.TrimPrefix(args[1], "--https="), args[2], onLine)
	default:
		onLine("fake tailscale: unexpected command")
		p.exit(2)
	}
	return p, nil
}

func (d *Daemon) runUp(p *Proc, onLine func(string)) {
	d.mu.Lock()
	fail := d.LoginFails
	d.mu.Unlock()
	if fail != "" {
		onLine(fail)
		p.exit(1)
		return
	}
	onLine("")
	onLine("To authenticate, visit:")
	onLine("")
	onLine("\t" + LoginURL)
	select {
	case <-d.login:
		d.mu.Lock()
		d.Backend = "Running"
		d.mu.Unlock()
		onLine("Success.")
		p.exit(0)
	case <-p.done:
	}
}

func (d *Daemon) runFunnel(p *Proc, port, target string, onLine func(string)) {
	d.mu.Lock()
	mode := d.Funnel
	d.mu.Unlock()
	switch mode {
	case FunnelFail:
		onLine("error: funnel not available")
		p.exit(1)
		return
	case FunnelApprovalExit:
		onLine("Funnel is not enabled on your tailnet.")
		onLine("To enable, visit:")
		onLine("         " + ApprovalURL)
		p.exit(0)
		return
	case FunnelHang:
		<-p.done
		return
	case FunnelApproval:
		onLine("Funnel is not enabled on your tailnet.")
		onLine("To enable, visit:")
		onLine("         " + ApprovalURL)
		select {
		case <-d.approve:
		case <-p.done:
			return
		}
	}
	n, _ := strconv.ParseUint(port, 10, 16)
	d.mu.Lock()
	select {
	case <-p.done:
		d.mu.Unlock()
		return
	default:
	}
	if d.portUsedLocked(port) {
		d.mu.Unlock()
		onLine("listener already exists for port " + port)
		p.exit(1)
		return
	}
	d.nextID++
	id := fmt.Sprintf("session-%d", d.nextID)
	host := strings.TrimSuffix(d.Host, ".")
	d.foreground[id] = entry(host, uint16(n), target)
	keep := d.KeepSessions
	p.mu.Lock()
	p.onStop = func() {
		if keep {
			return
		}
		d.mu.Lock()
		delete(d.foreground, id)
		d.mu.Unlock()
	}
	p.mu.Unlock()
	d.mu.Unlock()
	onLine("Available on the internet:")
	onLine("")
	onLine("https://" + host)
	onLine("|-- / proxy " + target)
	onLine("")
	onLine("Press Ctrl+C to exit.")
	<-p.done
}
