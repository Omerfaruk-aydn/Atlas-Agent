package localapps

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
)

// RuntimeStatus is the observed availability of a runtime.
type RuntimeStatus struct {
	Kind       string `json:"kind"`
	Available  bool   `json:"available"`
	Executable string `json:"executable,omitempty"`
	Version    string `json:"version,omitempty"`
	Problem    string `json:"problem,omitempty"`
	Remedy     string `json:"remedy,omitempty"`
}

// RuntimeChecker reports runtime availability. The integrations service
// implements it, so /app and /integrations share one answer.
type RuntimeChecker interface {
	CheckRuntime(ctx context.Context, kind string) RuntimeStatus
}

// Errors with stable codes for the agent and the UI.
var (
	ErrNotFound       = errors.New("app_not_found")
	ErrBusy           = errors.New("app_busy")
	ErrRuntimeMissing = errors.New("runtime_missing")
	ErrPortInUse      = errors.New("port_in_use")
	ErrEarlyExit      = errors.New("early_exit")
	ErrHealthTimeout  = errors.New("health_timeout")
	ErrStartFailed    = errors.New("start_failed")
	ErrNotOwned       = errors.New("not_owned")
	ErrInvalidState   = errors.New("invalid_state")
	ErrInvalidBackup  = errors.New("invalid_backup")
	ErrIncompatible   = errors.New("incompatible_backup")
	ErrUnsafePath     = errors.New("unsafe_path")
)

// OpError carries a stable code plus a readable message and next step.
type OpError struct {
	Code   error
	Msg    string
	Remedy string
	// LogTail is the app output around a start failure.
	LogTail []string
}

func (e *OpError) Error() string { return e.Msg }

// Unwrap exposes the code for errors.Is.
func (e *OpError) Unwrap() error { return e.Code }

func opErr(code error, remedy, format string, args ...any) *OpError {
	return &OpError{Code: code, Msg: fmt.Sprintf(format, args...), Remedy: remedy}
}

// Options configures a Manager.
type Options struct {
	// StateDir holds the registry, logs, backups and sealed secrets.
	StateDir string
	// AppsHome is where new apps are created by default.
	AppsHome string
	Runtime  RuntimeChecker
}

// Manager owns the registry, running app processes and operation journals.
type Manager struct {
	stateDir string
	home     string
	runtime  RuntimeChecker
	reg      registry
	broker   *pubsub.Broker[Event]

	mu       sync.Mutex
	procs    map[string]*runningApp
	busy     map[string]string
	journals map[string]*Journal
}

type runningApp struct {
	proc      *procown.Proc
	port      int
	url       string
	startedAt time.Time
}

// DefaultStateDir returns the per-user state directory for apps.
// ATLAS_APPS_DIR overrides it.
func DefaultStateDir(globalDataFile string) string {
	if dir := os.Getenv("ATLAS_APPS_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(globalDataFile), "apps")
}

// DefaultAppsHome is where new apps are created. ATLAS_APPS_HOME overrides
// it.
func DefaultAppsHome() string {
	if dir := os.Getenv("ATLAS_APPS_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "atlas-apps")
	}
	return filepath.Join(home, "atlas-apps")
}

// NewManager builds a manager.
func NewManager(opts Options) *Manager {
	return &Manager{
		stateDir: opts.StateDir,
		home:     cmp.Or(opts.AppsHome, DefaultAppsHome()),
		runtime:  opts.Runtime,
		reg:      registry{path: filepath.Join(opts.StateDir, "registry.json")},
		broker:   pubsub.NewBroker[Event](),
		procs:    map[string]*runningApp{},
		busy:     map[string]string{},
		journals: map[string]*Journal{},
	}
}

// SetRuntimeChecker replaces the runtime checker.
func (m *Manager) SetRuntimeChecker(rc RuntimeChecker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtime = rc
}

// AppsHome returns where new apps are created.
func (m *Manager) AppsHome() string { return m.home }

// StateDir returns the manager's state directory.
func (m *Manager) StateDir() string { return m.stateDir }

// Subscribe streams journal events.
func (m *Manager) Subscribe(ctx context.Context) <-chan pubsub.Event[Event] {
	return m.broker.Subscribe(ctx)
}

func (m *Manager) logDir(id string) string    { return filepath.Join(m.stateDir, "logs", id) }
func (m *Manager) backupDir(id string) string { return filepath.Join(m.stateDir, "backups", id) }
func (m *Manager) secretDir(id string) string { return filepath.Join(m.stateDir, "secrets", id) }

// LogPath returns the app's current log file.
func (m *Manager) LogPath(id string) string { return filepath.Join(m.logDir(id), "app.log") }

// acquire marks id busy with op. Every mutating operation holds it, so
// start, verify, backup and restore of one app never overlap.
func (m *Manager) acquire(id, op string) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.busy[id]; ok {
		return nil, opErr(ErrBusy, "Wait for it to finish, then try again.", "%s is already running for this app", cur)
	}
	m.busy[id] = op
	return func() {
		m.mu.Lock()
		delete(m.busy, id)
		m.mu.Unlock()
	}, nil
}

// Busy reports the operation running for id, if any.
func (m *Manager) Busy(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy[id]
}

func (m *Manager) checkRuntime(ctx context.Context, kind string) RuntimeStatus {
	m.mu.Lock()
	rc := m.runtime
	m.mu.Unlock()
	if rc == nil {
		return RuntimeStatus{Kind: kind, Problem: "No runtime checker is configured."}
	}
	return rc.CheckRuntime(ctx, kind)
}

// CheckRuntime exposes the shared runtime answer.
func (m *Manager) CheckRuntime(ctx context.Context, kind string) RuntimeStatus {
	return m.checkRuntime(ctx, kind)
}

// App is a record joined with its manifest and live process state.
type App struct {
	Record
	Manifest      *Manifest `json:"manifest,omitempty"`
	ManifestError string    `json:"manifest_error,omitempty"`
	Diagnostics   []string  `json:"diagnostics,omitempty"`
	Running       bool      `json:"running"`
	// Owner is "this_session" or "previous_session" while running.
	Owner string `json:"owner,omitempty"`
	URL   string `json:"url,omitempty"`
	Busy  string `json:"busy,omitempty"`
	// SourceChanged is set when files changed after the last verification
	// (only computed by Get).
	SourceChanged bool `json:"source_changed,omitempty"`
	// CurrentSourceHash binds integration checks to the observed source.
	CurrentSourceHash string `json:"-"`
	// PendingAcceptance lists criteria without passing evidence for the
	// current source.
	PendingAcceptance []string `json:"pending_acceptance,omitempty"`
	Tech              string   `json:"tech,omitempty"`
}

// List returns every registered app. A damaged manifest is reported on its
// own app and never hides the others.
func (m *Manager) List(ctx context.Context) ([]App, error) {
	recs, err := m.reg.read()
	if err != nil {
		return nil, err
	}
	out := make([]App, 0, len(recs))
	for _, r := range recs {
		out = append(out, m.view(r, true))
	}
	slices.SortFunc(out, func(a, b App) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return out, ctx.Err()
}

// Get returns one app, including whether its files changed since the last
// verification.
func (m *Manager) Get(ctx context.Context, id string) (App, error) {
	r, err := m.record(id)
	if err != nil {
		return App{}, err
	}
	return m.view(r, true), ctx.Err()
}

func (m *Manager) record(id string) (Record, error) {
	recs, err := m.reg.read()
	if err != nil {
		return Record{}, err
	}
	for _, r := range recs {
		if r.ID == id {
			return r, nil
		}
	}
	return Record{}, opErr(ErrNotFound, "List apps to see their ids.", "no app with id %q", id)
}

func (m *Manager) view(r Record, deep bool) App {
	a := App{Record: r, Busy: m.Busy(r.ID)}
	if rt, ok := RuntimeFor(r.Runtime); ok {
		a.Tech = rt.Stack
	}
	if _, err := os.Stat(r.Root); err != nil {
		a.Diagnostics = append(a.Diagnostics, "The app folder "+r.Root+" is missing.")
	} else if r.State != StateDesigning {
		man, err := LoadManifest(r.Root)
		switch {
		case err != nil:
			a.ManifestError = err.Error()
			a.Diagnostics = append(a.Diagnostics, "The manifest could not be used: "+err.Error())
		case man.ID != r.ID:
			a.ManifestError = fmt.Sprintf("manifest id %q does not match the registered id %q", man.ID, r.ID)
			a.Diagnostics = append(a.Diagnostics, "The manifest belongs to a different app ("+man.ID+"); Atlas will not run it under this entry.")
		default:
			a.Manifest = &man
			if r.ManifestHash != "" && manifestHash(r.Root) != r.ManifestHash {
				a.Diagnostics = append(a.Diagnostics, "The manifest changed after Atlas last recorded it; verify the app again.")
			}
		}
	}
	m.mu.Lock()
	live := m.procs[r.ID]
	m.mu.Unlock()
	if live != nil {
		select {
		case <-live.proc.Done():
		default:
			a.Running, a.Owner, a.URL = true, "this_session", live.url
		}
	}
	if !a.Running && r.Process != nil {
		if procown.Matches(r.Process.Identity) {
			a.Running, a.Owner, a.URL = true, "previous_session", r.Process.URL
		} else {
			a.Diagnostics = append(a.Diagnostics, "It was running when Atlas last ran and has stopped since.")
		}
	}
	if deep && a.Manifest != nil {
		hash, err := sourceHash(r.Root, a.Manifest.DataDirs)
		a.CurrentSourceHash = hash
		if err != nil {
			a.Diagnostics = append(a.Diagnostics, err.Error())
		}
		if err != nil || r.LastVerification != nil && r.LastVerification.SourceHash != hash {
			a.SourceChanged = true
			if a.State == StateReady || a.State == StateVerified {
				a.State = StateScaffolded
			}
		}
		if r.Spec != nil {
			for i, c := range r.Spec.Acceptance {
				ok := false
				for _, ac := range r.Acceptance {
					if err == nil && ac.Index == i+1 && ac.Passed && ac.SourceHash == hash {
						ok = true
					}
				}
				if !ok {
					a.PendingAcceptance = append(a.PendingAcceptance, c)
				}
			}
		}
	}
	return a
}

// mutate updates one record under the registry lock.
func (m *Manager) mutate(ctx context.Context, id string, fn func(*Record) error) (Record, error) {
	var out Record
	err := m.reg.update(ctx, func(recs []Record) ([]Record, error) {
		for i := range recs {
			if recs[i].ID == id {
				if err := fn(&recs[i]); err != nil {
					return nil, err
				}
				recs[i].UpdatedAt = time.Now()
				out = recs[i]
				return recs, nil
			}
		}
		return nil, opErr(ErrNotFound, "List apps to see their ids.", "no app with id %q", id)
	})
	return out, err
}

// PlanInput is the app definition the agent derived from the request.
type PlanInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Spec        Spec   `json:"spec"`
	// Root is an absolute empty or new folder; defaults to AppsHome/<id>.
	Root string `json:"root,omitempty"`
}

func randomSuffix() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Plan records the app definition after checking the runtime. Nothing is
// written to the app folder yet.
func (m *Manager) Plan(ctx context.Context, in PlanInput) (Record, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 80 {
		return Record{}, opErr(ErrInvalidState, "Give the app a short name.", "name is required (up to 80 characters)")
	}
	id := Slug(name) + "-" + randomSuffix()
	m.startJournal(id, "create")
	m.stage(id, StageDesigning, StageRunning, "")
	if err := in.Spec.Validate(); err != nil {
		m.stage(id, StageDesigning, StageFailed, err.Error())
		m.finishJournal(id, err)
		return Record{}, opErr(ErrInvalidState, "Fix the app definition and plan again.", "invalid app definition: %v", err)
	}
	rt := SupportedRuntimes()[0]
	m.stage(id, StageDependencies, StageRunning, rt.Name)
	status := m.checkRuntime(ctx, rt.Kind)
	if !status.Available {
		msg := cmp.Or(status.Problem, rt.Name+" is not available")
		m.stage(id, StageDependencies, StageFailed, msg)
		err := &OpError{Code: ErrRuntimeMissing, Msg: msg, Remedy: cmp.Or(status.Remedy, rt.Remedy)}
		m.finishJournal(id, err)
		return Record{}, err
	}
	m.stage(id, StageDependencies, StagePassed, strings.TrimSpace(status.Version+" "+status.Executable))
	root := in.Root
	if root == "" {
		root = filepath.Join(m.home, id)
	}
	if !filepath.IsAbs(root) {
		err := opErr(ErrUnsafePath, "Use an absolute folder path.", "root must be an absolute path")
		m.finishJournal(id, err)
		return Record{}, err
	}
	if err := emptyOrMissing(root); err != nil {
		m.stage(id, StageDesigning, StageFailed, err.Error())
		m.finishJournal(id, err)
		return Record{}, err
	}
	now := time.Now()
	spec := in.Spec
	rec := Record{ID: id, Root: filepath.Clean(root), Name: name, Description: strings.TrimSpace(in.Description), Runtime: rt.Kind, State: StateDesigning, CreatedByAtlas: true, RegisteredAt: now, UpdatedAt: now, Spec: &spec}
	err := m.reg.update(ctx, func(recs []Record) ([]Record, error) {
		for _, r := range recs {
			if samePath(r.Root, rec.Root) {
				return nil, opErr(ErrInvalidState, "Choose another folder.", "another app (%s) already uses %s", r.ID, rec.Root)
			}
		}
		return append(recs, rec), nil
	})
	if err != nil {
		m.finishJournal(id, err)
		return Record{}, err
	}
	m.stage(id, StageDesigning, StagePassed, fmt.Sprintf("%d data types, %d acceptance criteria", len(spec.Entities), len(spec.Acceptance)))
	return rec, nil
}

func emptyOrMissing(root string) error {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return opErr(ErrUnsafePath, "Choose an empty or new folder; Atlas never writes into a folder with files in it.", "%s is not empty", root)
	}
	return nil
}

func samePath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// Register adds an existing folder with a valid manifest. It is recorded
// as needing verification, never as ready.
func (m *Manager) Register(ctx context.Context, root string) (Record, error) {
	if !filepath.IsAbs(root) {
		return Record{}, opErr(ErrUnsafePath, "Use an absolute folder path.", "root must be an absolute path")
	}
	man, err := LoadManifest(root)
	if err != nil {
		return Record{}, opErr(ErrInvalidState, "Only folders with a valid "+ManifestFile+" for a supported runtime can be managed.", "cannot register %s: %v", root, err)
	}
	now := time.Now()
	rec := Record{ID: man.ID, Root: filepath.Clean(root), Name: man.Name, Description: man.Description, Runtime: man.Runtime, State: StateScaffolded, RegisteredAt: now, UpdatedAt: now, ManifestHash: manifestHash(root), Spec: man.Spec}
	err = m.reg.update(ctx, func(recs []Record) ([]Record, error) {
		for _, r := range recs {
			if r.ID == rec.ID || samePath(r.Root, rec.Root) {
				return nil, opErr(ErrInvalidState, "It is already in the list.", "the app %s is already registered at %s", r.ID, r.Root)
			}
		}
		return append(recs, rec), nil
	})
	return rec, err
}

// Unregister removes the registry entry only; files, data and backups stay.
func (m *Manager) Unregister(ctx context.Context, id string) (string, error) {
	release, err := m.acquire(id, "unregister")
	if err != nil {
		return "", err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return "", err
	}
	stopped, _ := m.stopLocked(ctx, rec)
	err = m.reg.update(ctx, func(recs []Record) ([]Record, error) {
		return slices.DeleteFunc(recs, func(r Record) bool { return r.ID == id }), nil
	})
	if err != nil {
		return "", err
	}
	msg := "Removed from the app list. Files stay in " + rec.Root + "; backups stay in " + m.backupDir(id) + "."
	if stopped == "stopped" {
		msg = "Stopped the app. " + msg
	}
	return msg, nil
}

// safeToDelete refuses roots that are not a dedicated app folder.
func safeToDelete(rec Record) error {
	root := filepath.Clean(rec.Root)
	if !filepath.IsAbs(root) || filepath.Dir(root) == root {
		return opErr(ErrUnsafePath, "", "refusing to delete %s", root)
	}
	if home, err := os.UserHomeDir(); err == nil && samePath(home, root) {
		return opErr(ErrUnsafePath, "", "refusing to delete the home folder")
	}
	man, err := LoadManifest(root)
	if err != nil || man.ID != rec.ID {
		return opErr(ErrUnsafePath, "Delete the folder yourself if you are sure.", "%s does not hold this app's manifest; Atlas will not delete it", root)
	}
	return nil
}

// DeleteFiles stops the app, keeps a final data backup, deletes the app
// folder and unregisters it. Backups are kept.
func (m *Manager) DeleteFiles(ctx context.Context, id string) (string, error) {
	release, err := m.acquire(id, "delete")
	if err != nil {
		return "", err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return "", err
	}
	if err := safeToDelete(rec); err != nil {
		return "", err
	}
	if _, err := m.stopLocked(ctx, rec); err != nil {
		return "", err
	}
	backup, err := m.backupLocked(ctx, rec, "before-delete", false)
	if err != nil {
		return "", fmt.Errorf("the safety backup failed, nothing was deleted: %w", err)
	}
	if err := os.RemoveAll(rec.Root); err != nil {
		return "", fmt.Errorf("delete %s: %w", rec.Root, err)
	}
	if err := m.reg.update(ctx, func(recs []Record) ([]Record, error) {
		return slices.DeleteFunc(recs, func(r Record) bool { return r.ID == id }), nil
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted %s and removed the app from the list. A data backup was kept: %s.", rec.Root, backup.Path), nil
}

// DeleteData stops the app, keeps a backup and empties its data folders.
// The app stays registered and can start again with empty data.
func (m *Manager) DeleteData(ctx context.Context, id string) (string, error) {
	release, err := m.acquire(id, "delete-data")
	if err != nil {
		return "", err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return "", err
	}
	man, err := LoadManifest(rec.Root)
	if err != nil {
		return "", err
	}
	if _, err := m.stopLocked(ctx, rec); err != nil {
		return "", err
	}
	backup, err := m.backupLocked(ctx, rec, "before-delete-data", false)
	if err != nil {
		return "", fmt.Errorf("the safety backup failed, nothing was deleted: %w", err)
	}
	for _, d := range man.DataDirs {
		dir, err := insideRoot(rec.Root, d)
		if err != nil {
			return "", err
		}
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("empty %s: %w", dir, err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	return "Deleted the app's data. A backup was kept: " + backup.Path + ".", nil
}

// insideRoot resolves rel under root and refuses anything that escapes it,
// including through a symlink.
func insideRoot(root, rel string) (string, error) {
	if err := relInside(rel); err != nil {
		return "", opErr(ErrUnsafePath, "", "%v", err)
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", opErr(ErrUnsafePath, "", "%s is a link; Atlas does not follow links out of the app folder", rel)
	}
	return p, nil
}

// pickPort returns a free localhost port, trying preferred first.
func pickPort(preferred int) (int, error) {
	if preferred > 0 && !listening(preferred) {
		return preferred, nil
	}
	for p := 18100; p < 18999; p++ {
		if p != preferred && !listening(p) {
			var lc net.ListenConfig
			ln, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
			if err != nil {
				continue
			}
			ln.Close()
			return p, nil
		}
	}
	return 0, errors.New("no free port between 18100 and 18999")
}

func listening(port int) bool {
	d := net.Dialer{Timeout: 300 * time.Millisecond}
	conn, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// StopAll stops apps started by this Atlas process. Atlas apps run while
// Atlas runs: on exit they are stopped (on Windows the job object would end
// them anyway), so nothing keeps listening without Atlas able to manage it.
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		rec, err := m.record(id)
		if err != nil {
			continue
		}
		if _, err := m.stopLocked(ctx, rec); err != nil {
			slog.Warn("Failed to stop local app on exit", "app", id, "error", err)
		}
	}
	m.broker.Shutdown()
}
