package isolation

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// Guest paths used inside the sandbox.
const (
	GuestBinDir      = `C:\AtlasBin`
	GuestExchangeDir = `C:\AtlasExchange`
	guestPort        = 7443
	// SandboxNodeName is the node name an Atlas sandbox registers under.
	SandboxNodeName = "sandbox"
)

// HostSnapshot is the host's input state, sampled around guest actions to
// prove they did not touch the user's session.
type HostSnapshot struct {
	ForegroundWindow uintptr `json:"foreground_window"`
	ForegroundTitle  string  `json:"foreground_title,omitempty"`
	CursorX          int     `json:"cursor_x"`
	CursorY          int     `json:"cursor_y"`
}

// Platform is the OS seam; tests replace it.
type Platform interface {
	Probe(ctx context.Context) Facts
	// Launch starts the sandbox for a .wsb file and returns the identities
	// of the processes that appeared because of it.
	Launch(ctx context.Context, wsb string) ([]procown.Identity, error)
	// Focus brings an owned sandbox window to the foreground for handoff.
	Focus(owned []procown.Identity) error
	HostSnapshot() (HostSnapshot, error)
}

// Manager owns isolated environments.
type Manager struct {
	store    *leaseStore
	platform Platform
	registry *nodes.Registry
	exe      string
	// ReadyTimeout bounds waiting for the guest agent.
	ReadyTimeout time.Duration
	now          func() time.Time
}

// NewManager uses devicesDir/isolation for leases and registers guests in
// the node registry at devicesDir. exe is the Atlas binary copied into the
// guest.
func NewManager(devicesDir, exe string, platform Platform) *Manager {
	if platform == nil {
		platform = DefaultPlatform()
	}
	return &Manager{store: newLeaseStore(filepath.Join(devicesDir, "isolation")), platform: platform, registry: nodes.OpenRegistry(devicesDir), exe: exe, ReadyTimeout: 4 * time.Minute, now: time.Now}
}

// Capabilities probes the host.
func (m *Manager) Capabilities(ctx context.Context) []Capability {
	return Evaluate(m.platform.Probe(ctx))
}

// StartOptions configures a new sandbox.
type StartOptions struct {
	MemoryMB int
	// Clipboard shares the host clipboard; off by default so the guest
	// cannot read what the user copies.
	Clipboard bool
}

// Status is a lease plus its persisted lifecycle state and liveness.
type Status struct {
	Lease  Lease            `json:"lease"`
	Record taskstate.Record `json:"state"`
	Alive  bool             `json:"alive"`
}

// WSB renders the sandbox configuration. Values are XML-escaped; networking
// stays on because the control channel needs the guest NIC.
func WSB(binDir, exchangeDir string, opts StartOptions) (string, error) {
	type mapped struct {
		HostFolder    string
		SandboxFolder string
		ReadOnly      string
	}
	type config struct {
		XMLName              xml.Name `xml:"Configuration"`
		VGpu                 string   `xml:"vGPU"`
		Networking           string   `xml:"Networking"`
		MappedFolders        []mapped `xml:"MappedFolders>MappedFolder"`
		LogonCommand         string   `xml:"LogonCommand>Command"`
		AudioInput           string   `xml:"AudioInput"`
		VideoInput           string   `xml:"VideoInput"`
		PrinterRedirection   string   `xml:"PrinterRedirection"`
		ClipboardRedirection string   `xml:"ClipboardRedirection"`
		MemoryInMB           int      `xml:"MemoryInMB,omitempty"`
	}
	clip := "Disable"
	if opts.Clipboard {
		clip = "Enable"
	}
	c := config{
		VGpu: "Default", Networking: "Enable",
		MappedFolders: []mapped{
			{HostFolder: binDir, SandboxFolder: GuestBinDir, ReadOnly: "true"},
			{HostFolder: exchangeDir, SandboxFolder: GuestExchangeDir, ReadOnly: "false"},
		},
		LogonCommand: GuestBinDir + `\atlas.exe node serve --isolated-exchange ` + GuestExchangeDir,
		AudioInput:   "Disable", VideoInput: "Disable", PrinterRedirection: "Disable", ClipboardRedirection: clip,
		MemoryInMB: opts.MemoryMB,
	}
	data, err := xml.MarshalIndent(c, "", "  ")
	return string(data), err
}

// provision mirrors cmd.isolatedProvision.
type provision struct {
	Token   string `json:"token"`
	Name    string `json:"name"`
	Port    int    `json:"port"`
	LeaseID string `json:"lease_id"`
}

type agentInfo struct {
	LeaseID     string   `json:"lease_id"`
	NodeID      string   `json:"node_id"`
	Fingerprint string   `json:"fingerprint"`
	Port        int      `json:"port"`
	Addresses   []string `json:"addresses"`
	Desktop     bool     `json:"desktop"`
	Reason      string   `json:"reason,omitempty"`
}

// Start creates and boots a sandbox, waits for its control agent and
// registers it as node "sandbox". It never falls back to the host desktop.
func (m *Manager) Start(ctx context.Context, opts StartOptions) (Status, error) {
	var sandbox Capability
	for _, c := range m.Capabilities(ctx) {
		if c.Backend == BackendWindowsSandbox {
			sandbox = c
		}
	}
	if !sandbox.Available {
		return Status{}, &UnavailableError{Capability: sandbox}
	}
	if existing, _ := m.Active(); existing != nil {
		return *existing, fmt.Errorf("an Atlas sandbox (%s) is already running; stop it first or keep using it", existing.Lease.ID)
	}
	id := "sbx-" + strconv.FormatInt(m.now().UnixMilli(), 36)
	dir := m.store.leaseDir(id)
	l := Lease{ID: id, Backend: BackendWindowsSandbox, Dir: dir, Exchange: filepath.Join(dir, "exchange"), ConfigWSB: filepath.Join(dir, "atlas.wsb"), NodeName: SandboxNodeName, CreatedAt: m.now().UnixMilli(), Networking: true, MemoryMB: opts.MemoryMB}
	if err := os.MkdirAll(filepath.Join(l.Exchange, "files"), 0o700); err != nil {
		return Status{}, err
	}
	rec, err := m.store.states.Create(taskstate.Record{ID: id, Kind: "isolation", State: taskstate.Preparing, Target: BackendWindowsSandbox})
	if err != nil {
		return Status{}, err
	}
	fail := func(cause error) (Status, error) {
		for _, o := range l.Owned {
			_ = procown.KillOwned(o)
		}
		rec, _ = m.store.states.Move(id, 0, taskstate.Failed, cause.Error())
		_ = m.store.save(l)
		return Status{Lease: l, Record: rec}, cause
	}
	binDir := filepath.Join(dir, "bin")
	if err := copyFile(m.exe, filepath.Join(binDir, "atlas.exe")); err != nil {
		return fail(fmt.Errorf("copy Atlas into the guest share: %w", err))
	}
	token := nodes.NewToken()
	if err := taskstate.WriteJSONAtomic(filepath.Join(l.Exchange, "provision.json"), provision{Token: token, Name: "atlas-sandbox", Port: guestPort, LeaseID: id}); err != nil {
		return fail(err)
	}
	wsb, err := WSB(binDir, l.Exchange, opts)
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(l.ConfigWSB, []byte(wsb), 0o600); err != nil {
		return fail(err)
	}
	owned, err := m.platform.Launch(ctx, l.ConfigWSB)
	l.Owned = owned
	if err != nil {
		return fail(fmt.Errorf("launch Windows Sandbox: %w", err))
	}
	if err := m.store.save(l); err != nil {
		return fail(err)
	}
	readyCtx, cancel := context.WithTimeout(ctx, m.ReadyTimeout)
	defer cancel()
	data, err := waitFile(readyCtx, filepath.Join(l.Exchange, "agent.json"), 500*time.Millisecond)
	if err != nil {
		return fail(fmt.Errorf("the guest control agent did not start within %s (sandbox window may show the reason): %w", m.ReadyTimeout, err))
	}
	var agent agentInfo
	if err := json.Unmarshal(data, &agent); err != nil || agent.LeaseID != id || agent.Fingerprint == "" {
		return fail(fmt.Errorf("guest agent identity is invalid or belongs to another lease"))
	}
	l.Fingerprint = nodes.NormalizeFingerprint(agent.Fingerprint)
	client, addr, err := connectGuest(readyCtx, agent, token)
	if err != nil {
		return fail(err)
	}
	l.Address = addr
	info, err := client.Info(readyCtx)
	if err != nil {
		return fail(err)
	}
	if err := m.registry.Put(nodes.Entry{Name: SandboxNodeName, URL: client.BaseURL, Fingerprint: l.Fingerprint, NodeID: info.NodeID, DeviceID: info.DeviceID, OS: info.OS, Arch: info.Arch, Root: info.Root, Capabilities: info.Capabilities, Network: nodes.NetworkLAN, Isolated: true, MaxFileBytes: info.MaxFileBytes}, token); err != nil {
		return fail(err)
	}
	if err := m.store.save(l); err != nil {
		return fail(err)
	}
	rec, err = m.store.states.Move(id, rec.Revision, taskstate.Running, "")
	if err != nil {
		return fail(err)
	}
	if !info.Desktop {
		slog.Warn("Sandbox agent started without desktop control", "reason", info.DesktopReason)
	}
	return Status{Lease: l, Record: rec, Alive: true}, nil
}

// allowLoopbackGuest lets tests stand in for a guest on 127.0.0.1. A real
// sandbox guest is never on the host's loopback.
var allowLoopbackGuest = false

func connectGuest(ctx context.Context, agent agentInfo, token string) (*nodes.Client, string, error) {
	var lastErr error
	for {
		for _, a := range agent.Addresses {
			if ip := net.ParseIP(a); ip == nil || (ip.IsLoopback() && !allowLoopbackGuest) {
				continue
			}
			addr := net.JoinHostPort(a, strconv.Itoa(agent.Port))
			c, err := nodes.NewClient("https://"+addr, agent.Fingerprint, token)
			if err != nil {
				lastErr = err
				continue
			}
			probe, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err = c.Info(probe)
			cancel()
			if err == nil {
				return c, addr, nil
			}
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr == nil {
				lastErr = errors.New("guest reported no usable address")
			}
			return nil, "", fmt.Errorf("cannot reach the guest agent: %w", lastErr)
		case <-time.After(time.Second):
		}
	}
}

// Active returns the running Atlas sandbox, if any.
func (m *Manager) Active() (*Status, error) {
	leases, err := m.store.list()
	if err != nil {
		return nil, err
	}
	for _, l := range leases {
		st, err := m.Status(l.ID)
		if err == nil && !st.Record.State.Terminal() {
			return &st, nil
		}
	}
	return nil, nil
}

// Status reconciles a lease with the live processes. A sandbox closed
// outside Atlas is recorded as failed, not left "running".
func (m *Manager) Status(id string) (Status, error) {
	l, err := m.store.load(id)
	if err != nil {
		return Status{}, err
	}
	rec, err := m.store.states.Get(id)
	if err != nil {
		return Status{}, err
	}
	alive := false
	for _, o := range l.Owned {
		if procown.Matches(o) {
			alive = true
		}
	}
	if !alive && !rec.State.Terminal() && rec.State != taskstate.Preparing {
		rec, _ = m.store.states.Move(id, 0, taskstate.Failed, "the sandbox was closed outside Atlas")
	}
	return Status{Lease: l, Record: rec, Alive: alive}, nil
}

// List returns every lease with its status.
func (m *Manager) List() ([]Status, error) {
	leases, err := m.store.list()
	if err != nil {
		return nil, err
	}
	var out []Status
	for _, l := range leases {
		if st, err := m.Status(l.ID); err == nil {
			out = append(out, st)
		}
	}
	return out, nil
}

// Pause marks the environment as handed to the user and focuses its window.
func (m *Manager) Pause(id, reason string) (Status, error) {
	st, err := m.Status(id)
	if err != nil {
		return st, err
	}
	if !st.Alive {
		return st, fmt.Errorf("sandbox %s is not running", id)
	}
	ferr := m.platform.Focus(st.Lease.Owned)
	rec, err := m.store.states.Move(id, 0, taskstate.Paused, reason)
	st.Record = rec
	if err != nil {
		return st, err
	}
	return st, ferr
}

// Resume returns a paused environment to automation.
func (m *Manager) Resume(id string) (Status, error) {
	st, err := m.Status(id)
	if err != nil {
		return st, err
	}
	rec, err := m.store.states.Move(id, 0, taskstate.Running, "")
	st.Record = rec
	return st, err
}

// Stop terminates only processes this lease owns, unregisters the guest
// node and deletes the lease directory Atlas created.
func (m *Manager) Stop(id string) (Status, error) {
	st, err := m.Status(id)
	if err != nil {
		return st, err
	}
	var errs []string
	for _, o := range st.Lease.Owned {
		if err := procown.KillOwned(o); err != nil && !errors.Is(err, procown.ErrNotOwned) {
			errs = append(errs, err.Error())
		}
	}
	if e, _, err := m.registry.Get(SandboxNodeName); err == nil && e.Fingerprint == st.Lease.Fingerprint {
		_ = m.registry.Remove(SandboxNodeName)
	}
	if !st.Record.State.Terminal() {
		st.Record, _ = m.store.states.Move(id, 0, taskstate.Completed, "stopped by Atlas")
	}
	// Remove the lease's files but keep the lifecycle record.
	if strings.HasPrefix(filepath.Clean(st.Lease.Dir), filepath.Clean(m.store.dir)) {
		if err := os.RemoveAll(st.Lease.Dir); err != nil {
			errs = append(errs, err.Error())
		}
	}
	st.Alive = false
	if len(errs) > 0 {
		return st, errors.New(strings.Join(errs, "; "))
	}
	return st, nil
}

// HostSnapshot samples the host's foreground window and cursor.
func (m *Manager) HostSnapshot() (HostSnapshot, error) { return m.platform.HostSnapshot() }

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
