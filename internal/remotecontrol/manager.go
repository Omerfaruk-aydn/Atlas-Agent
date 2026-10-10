package remotecontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// ApprovalRequest asks the user, through the permission system, to approve
// one change of remote scope. Summary is the full localized plan.
type ApprovalRequest struct {
	Action  string
	Target  string
	Summary string
	Hash    string
}

// Errors an Approver returns when it cannot ask the user. Remote changes are
// never approved implicitly, not even in a mode that skips tool prompts.
var (
	ErrApprovalBypassed = errors.New("remote changes need an explicit approval")
	ErrApprovalReadOnly = errors.New("the permission mode allows only read-only actions")
)

// Approver is the permission system as the manager needs it.
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (bool, error)
}

// Options configures a Manager.
type Options struct {
	// Dir holds known_hosts, sealed secrets and per-profile state.
	Dir      string
	Dialer   Dialer
	Approver Approver
	Resolver Resolver
	// Verifier checks the public side; Verifier{} is used when nil.
	Verifier PublicChecker
	// Artifacts supplies the relay binaries; Bundled{} when nil.
	Artifacts ArtifactSource
	// Text localizes plan text for the approval dialog.
	Text func(string) string
	// GatewayAttached reports whether this computer's phone gateway is
	// connected to the relay for host.
	GatewayAttached func(host string) bool
	Logger          *slog.Logger
	// HealthInterval, HealthAttempts, HTTPSInterval and HTTPSAttempts pace
	// post-install checks; tests shorten them.
	HealthInterval time.Duration
	HealthAttempts int
	HTTPSInterval  time.Duration
	HTTPSAttempts  int
	Now            func() time.Time
}

// Manager runs at most one remote operation at a time and keeps the latest
// result per profile for the UI, which polls Snapshot. Operations continue
// when the /rc dialog closes.
type Manager struct {
	opts     Options
	HostKeys *HostKeys
	Secrets  *Secrets

	mu       sync.Mutex
	profiles map[string]*profileState
	op       *operation
}

type profileState struct {
	report      *Report
	assessment  *Assessment
	plan        *Plan
	checks      map[string]Check
	lastCheck   time.Time
	err         *Error
	hostKey     *HostKeyDecision
	journal     *Journal
	diagnostics []string
	loaded      bool
}

type operation struct {
	kind      string
	profileID string
	step      string
	approval  bool
	started   time.Time
	cancel    context.CancelFunc
	done      chan struct{}
}

// Snapshot is a copy of a profile's state for rendering.
type Snapshot struct {
	Busy             bool
	OpKind           string
	OpStep           string
	OpStarted        time.Time
	AwaitingApproval bool

	Report      *Report
	Assessment  *Assessment
	Plan        *Plan
	Checks      map[string]Check
	LastCheck   time.Time
	Err         *Error
	HostKey     *HostKeyDecision
	Journal     *Journal
	Diagnostics []string
}

// NewManager creates a manager rooted at opts.Dir.
func NewManager(opts Options) *Manager {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Verifier == nil {
		opts.Verifier = Verifier{}
	}
	if opts.Artifacts == nil {
		opts.Artifacts = Bundled{}
	}
	if opts.HealthInterval == 0 {
		opts.HealthInterval = 3 * time.Second
	}
	if opts.HealthAttempts == 0 {
		opts.HealthAttempts = 20
	}
	if opts.HTTPSInterval == 0 {
		opts.HTTPSInterval = 5 * time.Second
	}
	if opts.HTTPSAttempts == 0 {
		opts.HTTPSAttempts = 24
	}
	userKnownHosts := ""
	if home, err := os.UserHomeDir(); err == nil {
		userKnownHosts = filepath.Join(home, ".ssh", "known_hosts")
	}
	m := &Manager{
		opts:     opts,
		HostKeys: &HostKeys{Path: filepath.Join(opts.Dir, "known_hosts"), UserPath: userKnownHosts},
		Secrets:  &Secrets{Dir: filepath.Join(opts.Dir, "secrets")},
		profiles: map[string]*profileState{},
	}
	if m.opts.Dialer == nil {
		m.opts.Dialer = &SSHDialer{HostKeys: m.HostKeys}
	}
	return m
}

func (m *Manager) profileDir(id string) (string, error) {
	if !ValidID(id) {
		return "", errors.New("invalid profile id")
	}
	return filepath.Join(m.opts.Dir, "profiles", id), nil
}

// KeyPath is the connector key file of a profile.
func (m *Manager) KeyPath(id string) (string, error) {
	dir, err := m.profileDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "connector.key"), nil
}

// EnsureKey creates the profile's connector key once and returns its path.
// The key never leaves this computer except over SSH to the server.
func (m *Manager) EnsureKey(id string) (string, error) {
	path, err := m.KeyPath(id)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := mobilerelay.GenerateKey(path); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	return path, nil
}

func (m *Manager) token(id string) (string, error) {
	dir, err := m.profileDir(id)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "install-token")
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) == 32 {
		return strings.TrimSpace(string(data)), nil
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	tok := hex.EncodeToString(b[:])
	return tok, taskstate.WriteFileAtomic(path, []byte(tok+"\n"), 0o600)
}

func (m *Manager) journalPath(id string) (string, error) {
	dir, err := m.profileDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "operation.json"), nil
}

// state returns the profile's state, loading its journal on first use. A
// journal still marked running without an operation in this process was
// interrupted (Atlas closed or crashed) and is recorded as such.
func (m *Manager) state(id string) *profileState {
	st := m.profiles[id]
	if st == nil {
		st = &profileState{checks: map[string]Check{}}
		m.profiles[id] = st
	}
	if !st.loaded {
		st.loaded = true
		if path, err := m.journalPath(id); err == nil {
			if j, err := loadJournal(path); err == nil && j != nil {
				if j.Running() && (m.op == nil || m.op.profileID != id) {
					j.Status = StatusInterrupted
					for i := range j.Steps {
						if j.Steps[i].Status == StatusRunning {
							j.Steps[i].Status = StatusInterrupted
						}
					}
					_ = saveJournal(path, j)
				}
				st.journal = j
			}
		}
	}
	return st
}

// Snapshot copies a profile's state.
func (m *Manager) Snapshot(id string) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.state(id)
	s := Snapshot{
		Report: st.report, Assessment: st.assessment, Plan: st.plan, LastCheck: st.lastCheck,
		Err: st.err, HostKey: st.hostKey, Checks: map[string]Check{}, Diagnostics: slices.Clone(st.diagnostics),
	}
	for k, v := range st.checks {
		s.Checks[k] = v
	}
	if st.journal != nil {
		j := *st.journal
		j.Steps = slices.Clone(st.journal.Steps)
		s.Journal = &j
	}
	if m.op != nil && m.op.profileID == id {
		s.Busy, s.OpKind, s.OpStep, s.OpStarted, s.AwaitingApproval = true, m.op.kind, m.op.step, m.op.started, m.op.approval
	}
	return s
}

// Busy reports whether any operation runs.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.op != nil
}

// Cancel stops the running operation. Completed remote steps stay applied
// and are listed in the journal.
func (m *Manager) Cancel() {
	m.mu.Lock()
	op := m.op
	m.mu.Unlock()
	if op != nil {
		op.cancel()
	}
}

// Wait blocks until the running operation, if any, has finished.
func (m *Manager) Wait() {
	m.mu.Lock()
	op := m.op
	m.mu.Unlock()
	if op != nil {
		<-op.done
	}
}

// ErrBusy is returned when an operation is already running.
var ErrBusy = errors.New("another remote operation is running")

// start launches fn as the single running operation.
func (m *Manager) start(kind string, p config.RemoteProfile, fn func(ctx context.Context, op *operation) error) error {
	if !ValidID(p.ID) {
		return errors.New("invalid profile id")
	}
	m.mu.Lock()
	if m.op != nil {
		m.mu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	op := &operation{kind: kind, profileID: p.ID, started: m.opts.Now(), cancel: cancel, done: make(chan struct{})}
	m.op = op
	st := m.state(p.ID)
	st.err, st.hostKey = nil, nil
	if kind != OpTest {
		st.diagnostics = nil
	}
	m.mu.Unlock()
	go func() {
		defer close(op.done)
		defer cancel()
		err := fn(ctx, op)
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil {
			st.err = asError(err)
			if d, ok := DecisionFrom(err); ok {
				st.hostKey = &d
				st.checks[CheckSSH] = Check{State: CheckFailed, At: m.opts.Now(), Detail: Msg("The SSH host key needs your decision.")}
			}
			m.opts.Logger.Info("Remote control operation ended", "kind", kind, "profile", p.ID, "code", st.err.Code)
		}
		m.op = nil
	}()
	return nil
}

func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	if _, ok := DecisionFrom(err); ok {
		var changed *HostKeyChangedError
		if errors.As(err, &changed) {
			return newError("host_key_changed", Msg("The server's SSH host key changed. The connection was stopped."), Msg("Compare the fingerprints with your provider before deciding."))
		}
		return newError("host_key_unknown", Msg("This server's SSH host key is not trusted yet."), Msg("Compare the fingerprint with your provider, then trust it."))
	}
	if errors.Is(err, context.Canceled) {
		return newError("canceled", Msg("Canceled."), Message{})
	}
	if errors.Is(err, ErrSecretMissing) {
		return newError("secret_missing", Msg("The saved SSH secret is missing."), Msg("Enter it again in the profile."))
	}
	// Errors from this package never include secrets, but foreign errors
	// are not shown verbatim either.
	return newError("internal", Msg("The operation failed unexpectedly."), Msg("Open Diagnostics for details."))
}

func (m *Manager) setStep(op *operation, step string) {
	m.mu.Lock()
	op.step = step
	m.mu.Unlock()
}

func (m *Manager) update(id string, fn func(st *profileState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m.state(id))
}

// diag appends sanitized diagnostic lines: known secrets are redacted and
// lines are bounded.
func (m *Manager) diag(id string, redact []string, lines ...string) {
	m.update(id, func(st *profileState) {
		for _, line := range lines {
			for _, s := range redact {
				if s != "" {
					line = strings.ReplaceAll(line, s, "[REDACTED]")
				}
			}
			for l := range strings.SplitSeq(line, "\n") {
				l = strings.TrimRight(l, "\r")
				if len(l) > 300 {
					l = l[:300] + "…"
				}
				st.diagnostics = append(st.diagnostics, l)
			}
		}
		if n := len(st.diagnostics); n > 400 {
			st.diagnostics = st.diagnostics[n-400:]
		}
	})
}

func (m *Manager) secret(p config.RemoteProfile) ([]byte, error) {
	if p.SecretRef == "" || p.SSHAuth == AuthAgent {
		return nil, nil
	}
	return m.Secrets.Get(p.SecretRef)
}

// connect dials and records the SSH check.
func (m *Manager) connect(ctx context.Context, op *operation, p config.RemoteProfile) (Runner, []string, error) {
	m.setStep(op, CheckSSH)
	secret, err := m.secret(p)
	if err != nil {
		return nil, nil, err
	}
	redact := []string{string(secret)}
	r, err := m.opts.Dialer.Dial(ctx, p, secret)
	clear(secret)
	if err != nil {
		if _, ok := DecisionFrom(err); !ok {
			e := asError(err)
			m.update(p.ID, func(st *profileState) {
				st.checks[CheckSSH] = Check{State: CheckFailed, At: m.opts.Now(), Detail: e.What, Next: e.Next}
			})
		}
		return nil, nil, err
	}
	m.update(p.ID, func(st *profileState) {
		st.checks[CheckSSH] = Check{State: CheckOK, At: m.opts.Now(), Detail: Msg("Connected as %s; host key verified.", p.SSHUser)}
	})
	return r, redact, nil
}

// TrustHostKey records the user's decision for the pending host key of a
// profile. A revoked key is never trusted.
func (m *Manager) TrustHostKey(id string, d HostKeyDecision) error {
	if d.Revoked {
		return errors.New("a revoked host key cannot be trusted")
	}
	if err := m.HostKeys.Trust(d.Address, d.key, d.Fingerprint); err != nil {
		return err
	}
	m.update(id, func(st *profileState) {
		st.hostKey = nil
		st.err = nil
	})
	m.opts.Logger.Info("Remote control host key trusted", "address", d.Address, "fingerprint", d.Fingerprint, "changed", d.Changed)
	return nil
}

// DismissHostKey drops a pending decision without trusting the key.
func (m *Manager) DismissHostKey(id string) {
	m.update(id, func(st *profileState) { st.hostKey = nil })
}

// Test runs the read-only checks: SSH and host key, the server preflight,
// the services, HTTPS and the relay link.
func (m *Manager) Test(p config.RemoteProfile) error {
	return m.start(OpTest, p, func(ctx context.Context, op *operation) error {
		r, redact, err := m.connect(ctx, op, p)
		if err != nil {
			return err
		}
		defer r.Close()
		rep, a, err := m.preflight(ctx, op, r, p)
		if err != nil {
			return err
		}
		m.update(p.ID, func(st *profileState) { st.plan = nil })
		if rep.Installed && rep.Managed {
			m.setStep(op, StepHealth)
			if res, err := (remoteStep{r: r}).run(ctx, StepHealth, logsScript, nil, nil); err == nil {
				m.diag(p.ID, redact, "--- relay logs ---", string(res.Stdout))
			}
		}
		m.verify(ctx, op, p, rep, 1)
		if !a.Ready() {
			return newError("blocked", Msg("The server is not ready for installation."), Msg("Resolve the items listed under Server check."))
		}
		return nil
	})
}

func (m *Manager) preflight(ctx context.Context, op *operation, r Runner, p config.RemoteProfile) (Report, Assessment, error) {
	m.setStep(op, "preflight")
	rep, err := RunPreflight(ctx, r, p, m.opts.Resolver)
	if err != nil {
		return Report{}, Assessment{}, err
	}
	keySHA := ""
	if path, err := m.KeyPath(p.ID); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			keySHA = sha256Hex(data)
			clear(data)
		}
	}
	token, _ := m.token(p.ID)
	a := Assess(rep, p, keySHA, token, m.opts.Now(), m.opts.Artifacts)
	m.update(p.ID, func(st *profileState) {
		st.report, st.assessment, st.lastCheck = &rep, &a, m.opts.Now()
		st.checks[CheckService] = serviceCheck(rep, m.opts.Now())
	})
	m.diag(p.ID, nil, fmt.Sprintf("Server: %s %s (%s), privilege %s, docker %s, compose %s", rep.OSName, rep.OSVersion, rep.Machine, rep.Privilege, orUnknown(rep.DockerVersion), orUnknown(rep.ComposeVersion)),
		fmt.Sprintf("DNS %s: %s (matches server: %t)", rep.DNS.Host, strings.Join(rep.DNS.Addresses, ", "), rep.DNS.Matches))
	return rep, a, nil
}

func serviceCheck(rep Report, now time.Time) Check {
	switch {
	case !rep.Installed || !rep.Managed:
		return Check{State: CheckUnknown, At: now, Detail: Msg("The relay is not installed on this server.")}
	case rep.Services["relay"] == "running" && rep.Services["caddy"] == "running":
		v := ""
		if rep.State != nil {
			v = rep.State.Version
		}
		return Check{State: CheckOK, At: now, Detail: Msg("Relay %s and Caddy are running.", orUnknown(v))}
	}
	return Check{State: CheckFailed, At: now, Detail: Msg("The services are not running (relay: %s, caddy: %s).", orUnknown(rep.Services["relay"]), orUnknown(rep.Services["caddy"])), Next: Msg("Install again to repair, or open Diagnostics.")}
}

// verify checks HTTPS and the relay link from this computer, retrying
// HTTPS while a certificate may still be issued.
func (m *Manager) verify(ctx context.Context, op *operation, p config.RemoteProfile, rep Report, attempts int) bool {
	if !rep.Installed || !rep.Managed {
		m.update(p.ID, func(st *profileState) {
			st.checks[CheckHTTPS] = Check{State: CheckUnknown, At: m.opts.Now(), Detail: Msg("Not checked: the relay is not installed.")}
			st.checks[CheckLink] = Check{State: CheckUnknown, At: m.opts.Now(), Detail: Msg("Not checked: the relay is not installed.")}
		})
		return false
	}
	m.setStep(op, StepHTTPS)
	var https Check
	for i := range attempts {
		if i > 0 {
			m.update(p.ID, func(st *profileState) {
				https.State = CheckPending
				st.checks[CheckHTTPS] = https
			})
			select {
			case <-ctx.Done():
				return false
			case <-time.After(m.opts.HTTPSInterval):
			}
		}
		https = m.opts.Verifier.HTTPS(ctx, p.PublicHost)
		if https.State == CheckOK {
			break
		}
	}
	m.update(p.ID, func(st *profileState) { st.checks[CheckHTTPS] = https })
	if https.State != CheckOK {
		m.update(p.ID, func(st *profileState) {
			st.checks[CheckLink] = Check{State: CheckUnknown, At: m.opts.Now(), Detail: Msg("Not checked: HTTPS is not working yet.")}
		})
		return false
	}
	m.setStep(op, StepLink)
	attached := m.opts.GatewayAttached != nil && m.opts.GatewayAttached(p.PublicHost)
	token := ""
	if path, err := m.KeyPath(p.ID); err == nil {
		token, _ = mobilerelay.ReadToken(path)
	}
	var link Check
	if token == "" && !attached {
		link = Check{State: CheckFailed, At: m.opts.Now(), Detail: Msg("This computer has no connector key for this profile."), Next: Msg("Install to create and upload one.")}
	} else {
		link = m.opts.Verifier.Link(ctx, p.PublicHost, token, attached)
	}
	m.update(p.ID, func(st *profileState) { st.checks[CheckLink] = link })
	return link.State == CheckOK
}

// Install checks the server, shows the plan for approval and applies it.
// An approval covers exactly one plan: if a later attempt computes a
// different plan, the user is asked again.
func (m *Manager) Install(p config.RemoteProfile) error {
	if _, err := m.EnsureKey(p.ID); err != nil {
		return err
	}
	return m.start(OpInstall, p, func(ctx context.Context, op *operation) error {
		r, redact, err := m.connect(ctx, op, p)
		if err != nil {
			return err
		}
		defer r.Close()
		rep, a, err := m.preflight(ctx, op, r, p)
		if err != nil {
			return err
		}
		if !a.Ready() {
			return newError("blocked", Msg("The server is not ready for installation."), Msg("Resolve the items listed under Server check."))
		}
		plan := BuildPlan(p, rep, a)
		m.update(p.ID, func(st *profileState) { st.plan = &plan })
		token, err := m.token(p.ID)
		if err != nil {
			return err
		}
		path, err := m.journalPath(p.ID)
		if err != nil {
			return err
		}
		m.mu.Lock()
		prev := m.state(p.ID).journal
		m.mu.Unlock()
		resume := prev != nil && prev.Kind == OpInstall && prev.Approved && prev.Plan != nil && plan.Within(*prev.Plan) &&
			slices.Contains([]string{StatusInterrupted, StatusCanceled, StatusFailed}, prev.Status)
		if !resume {
			if err := m.approve(ctx, op, OpInstall, plan.Target, plan.Describe(m.opts.Text), plan.Hash); err != nil {
				return err
			}
		}
		approved, approvedHash := plan, plan.Hash
		if resume {
			approved, approvedHash = *prev.Plan, prev.PlanHash
		}
		j := &Journal{Kind: OpInstall, ProfileID: p.ID, Token: token, PlanHash: approvedHash, Plan: &approved, Approved: true, Status: StatusRunning, Started: m.opts.Now()}
		for _, s := range plan.Steps() {
			j.Step(s)
		}
		keyPath, _ := m.KeyPath(p.ID)
		connectorKey, err := os.ReadFile(keyPath)
		if err != nil {
			return newError("key", Msg("This computer's connector key cannot be read."), Message{})
		}
		defer clear(connectorKey)
		tokenText, _ := mobilerelay.ReadToken(keyPath)
		redact = append(redact, tokenText)
		ex := &executor{m: m, op: op, p: p, j: j, path: path, s: remoteStep{r: r}, redact: redact}
		return ex.install(ctx, plan, connectorKey)
	})
}

func (m *Manager) approve(ctx context.Context, op *operation, action, target, summary, hash string) error {
	if m.opts.Approver == nil {
		return newError("no_approver", Msg("Remote changes need the permission system, which is unavailable here."), Message{})
	}
	m.mu.Lock()
	op.approval, op.step = true, "approval"
	m.mu.Unlock()
	ok, err := m.opts.Approver.Approve(ctx, ApprovalRequest{Action: action, Target: target, Summary: summary, Hash: hash})
	m.mu.Lock()
	op.approval = false
	m.mu.Unlock()
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, ErrApprovalBypassed):
		return newError("approval_bypassed", Msg("Yolo mode runs tools without asking, but changes on your server always need your explicit approval."), Msg("Turn yolo mode off (ctrl+y), then run it again."))
	case errors.Is(err, ErrApprovalReadOnly):
		return newError("approval_read_only", Msg("Plan mode allows only read-only actions, so nothing was changed on the server."), Msg("Switch the permission mode, then run it again."))
	case err != nil:
		return newError("approval", Msg("The permission request could not be shown."), Msg("Check the permission mode; Plan mode refuses remote changes."))
	}
	if !ok {
		return newError("denied", Msg("The plan was not approved; nothing was changed on the server."), Message{})
	}
	return nil
}

type executor struct {
	m      *Manager
	op     *operation
	p      config.RemoteProfile
	j      *Journal
	path   string
	s      remoteStep
	redact []string
	locked bool
}

func (e *executor) save() {
	if err := saveJournal(e.path, e.j); err != nil {
		e.m.opts.Logger.Warn("Failed to save remote control journal", "error", err)
	}
	j := *e.j
	j.Steps = slices.Clone(e.j.Steps)
	e.m.update(e.p.ID, func(st *profileState) { st.journal = &j })
}

func (e *executor) begin(step string) {
	e.m.setStep(e.op, step)
	s := e.j.Step(step)
	s.Status, s.Started, s.Detail = StatusRunning, e.m.opts.Now(), Message{}
	e.save()
}

func (e *executor) end(step, status string, detail Message) {
	s := e.j.Step(step)
	s.Status, s.Finished, s.Detail = status, e.m.opts.Now(), detail
	e.save()
	e.m.opts.Logger.Info("Remote control step", "profile", e.p.ID, "step", step, "status", status)
}

func (e *executor) record(step string, res Result) {
	if out := strings.TrimSpace(string(res.Stdout) + "\n" + string(res.Stderr)); out != "" {
		e.m.diag(e.p.ID, e.redact, "--- "+step+" ---", out)
	}
}

// finish closes the journal. A canceled or failed run releases the server
// lock with a fresh short context so another attempt is not blocked.
func (e *executor) finish(status string, err error) error {
	if e.locked {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if res, uerr := e.s.run(ctx, StepUnlock, unlockScript, []string{e.j.Token}, nil); uerr == nil {
			e.locked = false
			e.record(StepUnlock, res)
		}
		cancel()
	}
	if err != nil && (errors.Is(err, context.Canceled) || asError(err).Code == "canceled") {
		status = StatusCanceled
		err = newError("canceled", Msg("Canceled. Completed steps stay applied and are listed above."), Msg("Install again to continue; finished steps are checked and skipped."))
	}
	for i := range e.j.Steps {
		if e.j.Steps[i].Status == StatusRunning {
			e.j.Steps[i].Status = StatusFailed
			if status == StatusCanceled {
				e.j.Steps[i].Status = StatusCanceled
			}
		}
	}
	e.j.Status, e.j.Finished = status, e.m.opts.Now()
	if err != nil {
		e.j.Error = asError(err)
	}
	e.save()
	return err
}

// do runs one remote step and records its result.
func (e *executor) do(ctx context.Context, step, script string, args []string, stdin []byte) (Result, error) {
	e.begin(step)
	res, err := e.s.run(ctx, step, script, args, stdin)
	e.record(step, res)
	if err != nil {
		e.end(step, StatusFailed, asError(err).What)
		return res, err
	}
	e.end(step, StatusDone, Message{})
	return res, nil
}

func (e *executor) install(ctx context.Context, plan Plan, connectorKey []byte) error {
	if _, err := e.do(ctx, StepLock, lockScript, []string{e.j.Token}, nil); err != nil {
		return e.finish(StatusFailed, err)
	}
	e.locked = true
	if len(plan.Packages) > 0 {
		if _, err := e.do(ctx, StepPackages, packagesScript, plan.Packages, nil); err != nil {
			return e.finish(StatusFailed, err)
		}
	}
	if len(plan.FirewallRules) > 0 {
		if _, err := e.do(ctx, StepFirewall, firewallScript, plan.FirewallRules, nil); err != nil {
			return e.finish(StatusFailed, err)
		}
	}
	if plan.Update {
		stamp := e.m.opts.Now().UTC().Format("20060102T150405Z")
		if _, err := e.do(ctx, StepBackup, backupScript, []string{stamp}, nil); err != nil {
			return e.finish(StatusFailed, err)
		}
		e.j.Backup = stamp
		e.save()
	}

	e.begin(StepBinary)
	art, data, err := e.m.opts.Artifacts.Lookup(plan.Artifact.Arch)
	if err != nil || art.SHA256 != plan.Artifact.SHA256 {
		e.end(StepBinary, StatusFailed, Msg("The bundled relay failed its integrity check."))
		return e.finish(StatusFailed, newError("artifact", Msg("The bundled relay failed its integrity check."), Msg("Reinstall Atlas.")))
	}
	binaryChanged, err := e.s.upload(ctx, StepBinary, RemoteDir+"/bin/atlas-mobile-relay", data, "0755", "")
	if err != nil {
		e.end(StepBinary, StatusFailed, asError(err).What)
		return e.finish(StatusFailed, err)
	}
	e.end(StepBinary, doneOrSkipped(binaryChanged), Msg("SHA-256 %s… verified on the server.", short(art.SHA256)))

	keyChanged := false
	if plan.InstallKey || plan.ReplaceKey {
		e.begin(StepKey)
		keyChanged, err = e.s.upload(ctx, StepKey, RemoteKeyFile, connectorKey, "0600", relayUID+":"+relayUID)
		if err != nil {
			e.end(StepKey, StatusFailed, asError(err).What)
			return e.finish(StatusFailed, err)
		}
		e.end(StepKey, doneOrSkipped(keyChanged), Message{})
	}

	e.begin(StepConfig)
	caddyChanged := false
	for _, f := range []struct {
		name    string
		content string
	}{
		{"compose.yaml", composeFile},
		{"Caddyfile", caddyFile(plan.Email)},
		{".env", envFile(plan.PublicHost)},
	} {
		changed, err := e.s.upload(ctx, StepConfig, RemoteDir+"/"+f.name, []byte(f.content), "0644", "")
		if err != nil {
			e.end(StepConfig, StatusFailed, asError(err).What)
			return e.finish(StatusFailed, err)
		}
		if f.name == "Caddyfile" && changed {
			caddyChanged = true
		}
	}
	e.end(StepConfig, StatusDone, Message{})

	// Restart only what changed; `up -d` itself recreates a service only
	// when its Compose definition changed.
	var restart []string
	if binaryChanged || keyChanged {
		restart = append(restart, "relay")
	}
	if caddyChanged {
		restart = append(restart, "caddy")
	}
	if _, err := e.do(ctx, StepStart, startScript, restart, nil); err != nil {
		return e.recover(ctx, plan, err)
	}

	e.begin(StepHealth)
	svc, err := waitHealthy(ctx, e.s, e.m.opts.HealthInterval, e.m.opts.HealthAttempts)
	if err != nil {
		e.end(StepHealth, StatusFailed, asError(err).What)
		return e.recover(ctx, plan, err)
	}
	e.end(StepHealth, StatusDone, Msg("relay: %s, caddy: %s", svc["relay"], svc["caddy"]))

	e.begin(StepState)
	if _, err := e.s.upload(ctx, StepState, RemoteDir+"/state.json", stateJSON(plan, sha256Hex(connectorKey), e.m.opts.Now()), "0644", ""); err != nil {
		e.end(StepState, StatusFailed, asError(err).What)
		return e.finish(StatusFailed, err)
	}
	e.end(StepState, StatusDone, Message{})

	if _, err := e.do(ctx, StepUnlock, unlockScript, []string{e.j.Token}, nil); err != nil {
		return e.finish(StatusFailed, err)
	}
	e.locked = false

	rep := Report{Installed: true, Managed: true}
	e.m.update(e.p.ID, func(st *profileState) {
		st.checks[CheckService] = Check{State: CheckOK, At: e.m.opts.Now(), Detail: Msg("Relay %s and Caddy are running.", plan.Artifact.Version)}
	})
	e.begin(StepHTTPS)
	ok := e.m.verify(ctx, e.op, e.p, rep, e.m.opts.HTTPSAttempts)
	snap := e.m.Snapshot(e.p.ID)
	https, link := snap.Checks[CheckHTTPS], snap.Checks[CheckLink]
	e.end(StepHTTPS, checkStatus(https), https.Detail)
	e.end(StepLink, checkStatus(link), link.Detail)
	if ctx.Err() != nil {
		return e.finish(StatusCanceled, ctx.Err())
	}
	if !ok {
		failed := https
		if https.State == CheckOK {
			failed = link
		}
		return e.finish(StatusIncomplete, newError("not_verified", Msg("The relay is installed, but remote access is not verified yet: %s", failed.Detail.String()), failed.Next))
	}
	return e.finish(StatusSucceeded, nil)
}

func checkStatus(c Check) string {
	switch c.State {
	case CheckOK:
		return StatusDone
	case CheckUnknown:
		return StatusSkipped
	}
	return StatusFailed
}

func doneOrSkipped(changed bool) string {
	if changed {
		return StatusDone
	}
	return StatusSkipped
}

// recover restores the backup taken by this run when an update fails to
// start. A first installation has nothing to restore.
func (e *executor) recover(ctx context.Context, plan Plan, cause error) error {
	if !plan.Update || e.j.Backup == "" || ctx.Err() != nil {
		return e.finish(StatusFailed, cause)
	}
	e.j.Step(StepRollback)
	if _, err := e.do(ctx, StepRollback, rollbackScript, []string{e.j.Backup}, nil); err != nil {
		return e.finish(StatusFailed, newError("rollback_failed", Msg("The update failed and restoring the previous version also failed."), Msg("Open Diagnostics; the backup %s is still on the server.", e.j.Backup)))
	}
	if _, err := waitHealthy(ctx, e.s, e.m.opts.HealthInterval, e.m.opts.HealthAttempts); err != nil {
		return e.finish(StatusFailed, newError("rollback_unhealthy", Msg("The previous version was restored but its services are not running either."), Msg("Open Diagnostics for the service logs.")))
	}
	return e.finish(StatusRolledBack, newError("rolled_back", Msg("The update did not start, so the previous version was restored."), Msg("Open Diagnostics for the cause.")))
}

// Rollback restores the newest backup on the server after approval.
func (m *Manager) Rollback(p config.RemoteProfile) error {
	return m.start(OpRollback, p, func(ctx context.Context, op *operation) error {
		r, redact, err := m.connect(ctx, op, p)
		if err != nil {
			return err
		}
		defer r.Close()
		rep, _, err := m.preflight(ctx, op, r, p)
		if err != nil {
			return err
		}
		if !rep.Managed || len(rep.Backups) == 0 {
			return newError("no_backup", Msg("There is no backup to restore on this server."), Message{})
		}
		backup := slices.Max(rep.Backups)
		target := p.SSHUser + "@" + HostAddress(p.SSHHost, p.Port())
		summary := Msg("Restore the relay backup %s on %s and restart its services. Files written since that backup are replaced.", backup, target).Localize(m.opts.Text)
		if err := m.approve(ctx, op, OpRollback, target, summary, sha256Hex([]byte(backup+target))); err != nil {
			return err
		}
		token, err := m.token(p.ID)
		if err != nil {
			return err
		}
		path, _ := m.journalPath(p.ID)
		j := &Journal{Kind: OpRollback, ProfileID: p.ID, Token: token, Approved: true, Status: StatusRunning, Backup: backup, Started: m.opts.Now()}
		for _, s := range []string{StepLock, StepRollback, StepHealth, StepUnlock} {
			j.Step(s)
		}
		ex := &executor{m: m, op: op, p: p, j: j, path: path, s: remoteStep{r: r}, redact: redact}
		if _, err := ex.do(ctx, StepLock, lockScript, []string{token}, nil); err != nil {
			return ex.finish(StatusFailed, err)
		}
		ex.locked = true
		if _, err := ex.do(ctx, StepRollback, rollbackScript, []string{backup}, nil); err != nil {
			return ex.finish(StatusFailed, err)
		}
		ex.begin(StepHealth)
		if _, err := waitHealthy(ctx, ex.s, m.opts.HealthInterval, m.opts.HealthAttempts); err != nil {
			ex.end(StepHealth, StatusFailed, asError(err).What)
			return ex.finish(StatusFailed, err)
		}
		ex.end(StepHealth, StatusDone, Message{})
		if _, err := ex.do(ctx, StepUnlock, unlockScript, []string{token}, nil); err != nil {
			return ex.finish(StatusFailed, err)
		}
		ex.locked = false
		return ex.finish(StatusSucceeded, nil)
	})
}

// RemoveService deletes the relay from the server after approval. It does
// not delete the local profile, revoke phones or stop provider billing.
func (m *Manager) RemoveService(p config.RemoteProfile) error {
	return m.start(OpRemove, p, func(ctx context.Context, op *operation) error {
		r, redact, err := m.connect(ctx, op, p)
		if err != nil {
			return err
		}
		defer r.Close()
		rep, _, err := m.preflight(ctx, op, r, p)
		if err != nil {
			return err
		}
		if rep.Installed && !rep.Managed {
			return newError("not_managed", Msg("%s was not created by /rc; Atlas will not delete it.", RemoteDir), Message{})
		}
		target := p.SSHUser + "@" + HostAddress(p.SSHHost, p.Port())
		summary := Msg("Remove the Atlas relay from %s: stop and delete the Compose project %s with its volumes (including Caddy certificates), delete %s and %s. Installed packages, firewall rules and other services are kept. The server itself keeps running and billing continues until you delete it at your provider.", target, ComposeName, RemoteDir, RemoteKeyFile).Localize(m.opts.Text)
		if err := m.approve(ctx, op, OpRemove, target, summary, sha256Hex([]byte("remove"+target))); err != nil {
			return err
		}
		token, _ := m.token(p.ID)
		path, _ := m.journalPath(p.ID)
		j := &Journal{Kind: OpRemove, ProfileID: p.ID, Token: token, Approved: true, Status: StatusRunning, Started: m.opts.Now()}
		ex := &executor{m: m, op: op, p: p, j: j, path: path, s: remoteStep{r: r}, redact: redact}
		if _, err := ex.do(ctx, StepRemove, removeScript, nil, nil); err != nil {
			return ex.finish(StatusFailed, err)
		}
		m.update(p.ID, func(st *profileState) {
			now := m.opts.Now()
			st.checks[CheckService] = Check{State: CheckUnknown, At: now, Detail: Msg("The relay is not installed on this server.")}
			st.checks[CheckHTTPS] = Check{State: CheckUnknown, At: now, Detail: Msg("Not checked: the relay is not installed.")}
			st.checks[CheckLink] = Check{State: CheckUnknown, At: now, Detail: Msg("Not checked: the relay is not installed.")}
		})
		return ex.finish(StatusSucceeded, nil)
	})
}

// ForgetProfile deletes this computer's state for a profile: its sealed
// secret, connector key, journal and install token. It does not contact the
// server; removing the remote service is a separate action.
func (m *Manager) ForgetProfile(p config.RemoteProfile) error {
	m.mu.Lock()
	if m.op != nil && m.op.profileID == p.ID {
		m.mu.Unlock()
		return ErrBusy
	}
	delete(m.profiles, p.ID)
	m.mu.Unlock()
	if p.SecretRef != "" {
		if err := m.Secrets.Delete(p.SecretRef); err != nil {
			return err
		}
	}
	dir, err := m.profileDir(p.ID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
