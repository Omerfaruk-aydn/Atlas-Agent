package tsfunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
)

// Phase is where a Funnel connection stands. /rc shows it as is.
type Phase string

// Phases.
const (
	PhaseIdle     Phase = ""
	PhaseChecking Phase = "checking"
	// PhasePanel starts the local phone panel; PhasePanelReady means it
	// listens on loopback and the Funnel is being set up.
	PhasePanel      Phase = "panel"
	PhasePanelReady Phase = "panel_ready"
	// PhaseApproval waits for the user to enable HTTPS or Funnel on
	// Tailscale's own page.
	PhaseApproval Phase = "approval"
	// PhaseReady means tailscaled lists Atlas' Funnel entry. It does not
	// mean a phone has reached it; the gateway reports that separately.
	PhaseReady   Phase = "ready"
	PhaseLost    Phase = "lost"
	PhaseStopped Phase = "stopped"
	PhaseFailed  Phase = "failed"
)

// Progress is shared by a running start and the UI, which reads it on
// every refresh. All methods are safe for concurrent use.
type Progress struct {
	mu       sync.Mutex
	phase    Phase
	approval string
	lines    []string
	detail   string
}

// NewProgress returns a progress at PhaseChecking.
func NewProgress() *Progress { return &Progress{phase: PhaseChecking} }

// Set changes the phase.
func (p *Progress) Set(ph Phase) {
	p.mu.Lock()
	p.phase = ph
	p.mu.Unlock()
}

func (p *Progress) fail(ph Phase, detail string) {
	p.mu.Lock()
	p.phase, p.detail = ph, detail
	p.mu.Unlock()
}

// Snapshot returns the phase, an approval URL and a short detail.
func (p *Progress) Snapshot() (Phase, string, string) {
	if p == nil {
		return PhaseIdle, "", ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.phase, p.approval, p.detail
}

func (p *Progress) line(s string) {
	s = Sanitize(s)
	if s == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if u := findLoginURL(s); u != "" && p.approval == "" {
		p.approval = u
	}
	p.lines = append(p.lines, s)
	if len(p.lines) > 12 {
		p.lines = p.lines[len(p.lines)-12:]
	}
}

func (p *Progress) approvalURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.approval
}

func (p *Progress) lastLines() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var keep []string
	for _, l := range p.lines {
		// Drop the CLI's decorative tree and blank-ish lines.
		if strings.HasPrefix(l, "|") || strings.HasPrefix(l, "Press Ctrl+C") {
			continue
		}
		keep = append(keep, l)
	}
	return Sanitize(strings.Join(keep[max(0, len(keep)-3):], " "))
}

var urlPattern = regexp.MustCompile(`https://[^\s"'<>]+`)

func findLoginURL(s string) string {
	for _, raw := range urlPattern.FindAllString(s, -1) {
		if u := loginURL(strings.TrimRight(raw, ".,;)")); u != "" {
			return u
		}
	}
	return ""
}

// PortConflictError means every Funnel port is used by something else.
// Atlas never replaces those entries.
type PortConflictError struct{ Occupants []Occupant }

func (e *PortConflictError) Error() string {
	var parts []string
	for _, o := range e.Occupants {
		parts = append(parts, fmt.Sprintf("%d: %s", o.Port, o.What))
	}
	return "every Funnel port is already used by another Serve or Funnel entry (" + strings.Join(parts, "; ") + ")"
}

// ApprovalRequiredError means the CLI asked the user to enable HTTPS or
// Funnel in the browser and exited without waiting.
type ApprovalRequiredError struct{ URL string }

func (e *ApprovalRequiredError) Error() string {
	return "Tailscale needs your approval in the browser before Funnel can start"
}

// ErrResidual means Atlas' own Funnel entry is still listed after its CLI
// stopped. Atlas does not remove entries by force.
var ErrResidual = errors.New("tailscale still lists Atlas' Funnel entry")

// Plan is a chosen public address.
type Plan struct {
	Host string
	Port uint16
}

// HostHeader is the Host header phones send: the port only when not 443.
func (p Plan) HostHeader() string {
	if p.Port == 443 {
		return p.Host
	}
	return net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port)))
}

// URL is the address phones open.
func (p Plan) URL() string { return "https://" + p.HostHeader() }

// Manager starts and stops Atlas' Funnel entry.
type Manager struct {
	CLI Exec
	// StateFile records the owned CLI process and session so a crashed
	// run can be cleaned up on the next start.
	StateFile string
	// Poll, ConfirmTimeout, ApprovalTimeout and CleanupTimeout default to
	// production values; tests shorten them.
	Poll            time.Duration
	ConfirmTimeout  time.Duration
	ApprovalTimeout time.Duration
	CleanupTimeout  time.Duration
}

func (m *Manager) poll() time.Duration    { return orDefault(m.Poll, 400*time.Millisecond) }
func (m *Manager) confirm() time.Duration { return orDefault(m.ConfirmTimeout, 30*time.Second) }
func (m *Manager) approve() time.Duration { return orDefault(m.ApprovalTimeout, 10*time.Minute) }
func (m *Manager) cleanup() time.Duration { return orDefault(m.CleanupTimeout, 6*time.Second) }

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// record is the ownership file. It holds no secret.
type record struct {
	Version   int              `json:"version"`
	Process   procown.Identity `json:"process"`
	Session   string           `json:"session"`
	Host      string           `json:"host"`
	Port      uint16           `json:"port"`
	Target    string           `json:"target"`
	StartedAt time.Time        `json:"started_at"`
}

// Prepare cleans up after a crashed run and picks the first free Funnel
// port. It changes nothing in Tailscale.
func (m *Manager) Prepare(ctx context.Context, r Readiness) (Plan, error) {
	if !r.Ready() {
		return Plan{}, fmt.Errorf("tailscale is not ready: %s", r.Problem)
	}
	if err := m.Recover(ctx); err != nil {
		return Plan{}, err
	}
	sc, err := readServe(ctx, m.CLI)
	if err != nil {
		return Plan{}, err
	}
	used := sc.occupants()
	allowed := r.FunnelPorts
	if len(allowed) == 0 {
		allowed = FunnelPorts
	}
	var conflicts []Occupant
	for _, port := range allowed {
		if o, busy := used[port]; busy {
			conflicts = append(conflicts, o)
			continue
		}
		return Plan{Host: r.Host, Port: port}, nil
	}
	return Plan{}, &PortConflictError{Occupants: conflicts}
}

// Start runs "tailscale funnel --https=<port> http://127.0.0.1:<local>" in
// the foreground and returns once tailscaled lists exactly that entry. The
// returned Link owns the CLI process; closing it removes the entry.
func (m *Manager) Start(ctx context.Context, plan Plan, localPort int, progress *Progress) (*Link, error) {
	if progress == nil {
		progress = NewProgress()
	}
	if localPort <= 0 || localPort > 65535 || !slices.Contains(FunnelPorts, plan.Port) || checkFunnelHost(plan.Host) != nil {
		return nil, fmt.Errorf("invalid Funnel plan %s → %d", plan.URL(), localPort)
	}
	target := "http://127.0.0.1:" + strconv.Itoa(localPort)
	// No --bg: the entry lives only as long as Atlas' process. No --yes:
	// an unexpected prompt fails instead of being confirmed silently.
	proc, err := m.CLI.Start([]string{"funnel", "--https=" + strconv.Itoa(int(plan.Port)), target}, progress.line)
	if err != nil {
		return nil, err
	}
	stop := func() { proc.Stop(3 * time.Second) }
	started := time.Now()
	deadline := started.Add(m.confirm())
	extended := false
	tick := time.NewTicker(m.poll())
	defer tick.Stop()
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			stop()
			return nil, ctx.Err()
		case <-proc.Done():
			if u := progress.approvalURL(); u != "" {
				return nil, &ApprovalRequiredError{URL: u}
			}
			code, _ := proc.Result()
			detail := progress.lastLines()
			if strings.Contains(detail, "listener already exists") || strings.Contains(detail, "already exists for port") {
				return nil, &PortConflictError{Occupants: []Occupant{{Port: plan.Port, What: detail}}}
			}
			return nil, fmt.Errorf("tailscale funnel exited (code %d): %s", code, detail)
		case <-tick.C:
		}
		if !extended && progress.approvalURL() != "" {
			extended = true
			progress.Set(PhaseApproval)
			deadline = started.Add(m.approve())
		}
		sc, err := readServe(ctx, m.CLI)
		if err == nil {
			if id, ok := sc.owned(plan.Host, plan.Port, target); ok {
				l := &Link{m: m, plan: plan, target: target, session: id, proc: proc, progress: progress}
				rec := record{Version: 1, Process: proc.Identity(), Session: id, Host: plan.Host, Port: plan.Port, Target: target, StartedAt: time.Now().UTC()}
				if err := m.writeRecord(rec); err != nil {
					stop()
					return nil, fmt.Errorf("record the Funnel owner: %w", err)
				}
				progress.Set(PhaseReady)
				go l.watch()
				return l, nil
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			stop()
			if lastErr != nil {
				return nil, fmt.Errorf("%w: Funnel entry did not appear: %v", ErrTimeout, lastErr)
			}
			return nil, fmt.Errorf("%w: Funnel entry did not appear", ErrTimeout)
		}
	}
}

// Recover handles a run that ended without Close, for example a crash.
// It stops only the CLI process Atlas recorded (matched by PID and
// creation time) and waits for its session to disappear. It never
// removes or edits any Serve or Funnel entry itself.
func (m *Manager) Recover(ctx context.Context) error {
	rec, ok, err := m.readRecord()
	if err != nil || !ok {
		return err
	}
	if procown.Matches(rec.Process) {
		_ = procown.KillOwned(rec.Process)
	}
	gone, err := m.waitGone(ctx, rec.Session)
	if err != nil {
		return err
	}
	if !gone {
		return fmt.Errorf("%w (%s, session %s); restart Tailscale or remove it with the Tailscale CLI", ErrResidual, Plan{Host: rec.Host, Port: rec.Port}.URL(), Sanitize(rec.Session))
	}
	return m.removeRecord()
}

func (m *Manager) waitGone(ctx context.Context, session string) (bool, error) {
	deadline := time.Now().Add(m.cleanup())
	for {
		sc, err := readServe(ctx, m.CLI)
		if err != nil {
			return false, err
		}
		if !sc.hasSession(session) {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(m.poll()):
		}
	}
}

func (m *Manager) readRecord() (record, bool, error) {
	if m.StateFile == "" {
		return record{}, false, nil
	}
	data, err := os.ReadFile(m.StateFile)
	if errors.Is(err, os.ErrNotExist) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, err
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil || rec.Version != 1 || rec.Session == "" {
		// A damaged record names nothing Atlas could safely act on.
		_ = os.Remove(m.StateFile)
		return record{}, false, nil
	}
	return rec, true, nil
}

func (m *Manager) writeRecord(rec record) error {
	if m.StateFile == "" {
		return nil
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.StateFile), 0o700); err != nil {
		return err
	}
	tmp := m.StateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.StateFile)
}

func (m *Manager) removeRecord() error {
	if m.StateFile == "" {
		return nil
	}
	if err := os.Remove(m.StateFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Link is a running Funnel entry owned by Atlas.
type Link struct {
	m        *Manager
	plan     Plan
	target   string
	session  string
	proc     Process
	progress *Progress
	closing  atomic.Bool
	once     sync.Once
	err      error
}

// URL is the public HTTPS address.
func (l *Link) URL() string { return l.plan.URL() }

// Plan returns the public host and port.
func (l *Link) Plan() Plan { return l.plan }

// Session is tailscaled's foreground session id for this entry.
func (l *Link) Session() string { return l.session }

// Target is the loopback URL Funnel forwards to.
func (l *Link) Target() string { return l.target }

// Progress returns the shared progress, including PhaseLost.
func (l *Link) Progress() *Progress { return l.progress }

// Alive reports whether the CLI that holds the entry still runs.
func (l *Link) Alive() bool {
	select {
	case <-l.proc.Done():
		return false
	default:
		return true
	}
}

func (l *Link) watch() {
	<-l.proc.Done()
	if l.closing.Load() {
		return
	}
	code, _ := l.proc.Result()
	l.progress.fail(PhaseLost, fmt.Sprintf("tailscale funnel exited (code %d): %s", code, l.progress.lastLines()))
}

// Close stops the CLI, waits until tailscaled no longer lists the
// session and removes the ownership record. It is idempotent.
func (l *Link) Close() error {
	l.once.Do(func() {
		l.closing.Store(true)
		l.proc.Stop(3 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), l.m.cleanup()+CommandTimeout)
		defer cancel()
		gone, err := l.m.waitGone(ctx, l.session)
		switch {
		case err != nil && errors.Is(err, ErrServiceUnavailable):
			// tailscaled is gone, and with it every foreground session.
			l.err = l.m.removeRecord()
		case err != nil:
			l.err = err
		case !gone:
			l.err = fmt.Errorf("%w (%s)", ErrResidual, l.plan.URL())
		default:
			l.err = l.m.removeRecord()
		}
		l.progress.Set(PhaseStopped)
	})
	return l.err
}
