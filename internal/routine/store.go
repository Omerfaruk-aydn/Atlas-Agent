package routine

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
)

// Permission policies a routine can run under. Nobody is present to answer
// a prompt during a scheduled run, so a call that would need an answer is
// denied unless the policy approves it up front.
const (
	// PermissionsConfigured keeps the user's configured permission mode and
	// allowed-tools list; anything that would need a prompt is denied. This
	// is the default.
	PermissionsConfigured = "configured"
	// PermissionsReadOnly runs in plan mode: only read-only tools work.
	PermissionsReadOnly = "readonly"
	// PermissionsEdits additionally approves file edits; shell and network
	// calls still need to be allowed by configuration.
	PermissionsEdits = "edits"
	// PermissionsAuto approves every tool call. It must be chosen
	// explicitly when the routine is created.
	PermissionsAuto = "auto"
)

// Trigger names recorded in the log.
const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// Run statuses recorded in the log.
const (
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusTimedOut  = "timed_out"
	StatusSkipped   = "skipped"
)

const (
	// DefaultTimeout bounds one run when the routine sets no timeout.
	DefaultTimeout = 30 * time.Minute
	// MaxTimeout is the longest timeout a routine may ask for.
	MaxTimeout = 24 * time.Hour

	maxPromptBytes  = 32 * 1024
	maxOutputBytes  = 64 * 1024
	maxLogEntries   = 200
	logCompactBytes = 1 << 20
	logKeepBytes    = 512 << 10
	maxRoutines     = 256
	claimGraceSlack = time.Minute
)

// ErrNotFound reports that no routine matches an id or name.
var ErrNotFound = errors.New("routine not found")

// ErrBusy reports that a routine is already running.
var ErrBusy = errors.New("routine is already running")

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Routine is one scheduled prompt.
type Routine struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Prompt   string `json:"prompt"`
	// Dir is the absolute working directory the run starts in.
	Dir string `json:"dir"`
	// Model optionally overrides the large model ("model" or
	// "provider/model").
	Model string `json:"model,omitempty"`
	// Goal runs the prompt as a goal: the agent keeps taking turns until a
	// second model confirms the goal is met or its budget runs out.
	Goal        bool   `json:"goal,omitempty"`
	Permissions string `json:"permissions"`
	// TimeoutSeconds bounds one run; zero means DefaultTimeout.
	TimeoutSeconds int  `json:"timeout_seconds,omitempty"`
	Enabled        bool `json:"enabled"`
	// MaxLateSeconds, when positive, makes the scheduler skip an
	// occurrence it discovers more than this long after it was due, such as
	// after the machine slept through it.
	MaxLateSeconds int `json:"max_late_seconds,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	// LastSlot is the scheduled occurrence most recently handled. The next
	// due time is computed from it, so occurrences missed while nothing was
	// running collapse into one run instead of a burst.
	LastSlot   time.Time `json:"last_slot,omitempty"`
	LastRunAt  time.Time `json:"last_run_at,omitempty"`
	LastStatus string    `json:"last_status,omitempty"`
	// RunningSince and RunningUntil mark a run in flight so two schedulers
	// never overlap runs of one routine. The lease expires on its own if the
	// process holding it dies.
	RunningSince time.Time `json:"running_since,omitempty"`
	RunningUntil time.Time `json:"running_until,omitempty"`
}

// Timeout returns the effective per-run timeout.
func (r Routine) Timeout() time.Duration {
	if r.TimeoutSeconds <= 0 {
		return DefaultTimeout
	}
	return time.Duration(r.TimeoutSeconds) * time.Second
}

// ParsedSchedule parses the routine's cron expression.
func (r Routine) ParsedSchedule() (Schedule, error) { return ParseSchedule(r.Schedule) }

// NextAfter returns the next scheduled time after t.
func (r Routine) NextAfter(t time.Time) (time.Time, error) {
	s, err := r.ParsedSchedule()
	if err != nil {
		return time.Time{}, err
	}
	return s.Next(t), nil
}

// DueSlot reports the scheduled occurrence that is due at now, if any.
func (r Routine) DueSlot(now time.Time) (time.Time, bool) {
	if !r.Enabled {
		return time.Time{}, false
	}
	base := r.LastSlot
	if base.IsZero() {
		base = r.CreatedAt
	}
	sched, err := r.ParsedSchedule()
	if err != nil {
		return time.Time{}, false
	}
	// Walk to the newest occurrence that has passed: several missed
	// occurrences are one due run, not a burst.
	var slot time.Time
	for range maxSlotWalk {
		next := sched.Next(base)
		if next.IsZero() || next.After(now) {
			break
		}
		slot, base = next, next
	}
	return slot, !slot.IsZero()
}

// maxSlotWalk bounds the search for the newest due occurrence.
const maxSlotWalk = 200_000

// Running reports whether a live lease marks the routine as running.
func (r Routine) Running(now time.Time) bool {
	return !r.RunningSince.IsZero() && now.Before(r.RunningUntil)
}

// Validate checks a routine definition.
func (r Routine) Validate() error {
	if !nameRE.MatchString(r.Name) {
		return fmt.Errorf("routine name %q must be 1-64 letters, digits, '.', '_' or '-'", r.Name)
	}
	if _, err := ParseSchedule(r.Schedule); err != nil {
		return err
	}
	if strings.TrimSpace(r.Prompt) == "" {
		return errors.New("routine prompt is empty")
	}
	if len(r.Prompt) > maxPromptBytes {
		return fmt.Errorf("routine prompt is longer than %d bytes", maxPromptBytes)
	}
	if !filepath.IsAbs(r.Dir) {
		return fmt.Errorf("routine directory %q must be absolute", r.Dir)
	}
	switch r.Permissions {
	case PermissionsConfigured, PermissionsReadOnly, PermissionsEdits, PermissionsAuto:
	default:
		return fmt.Errorf("unknown permissions policy %q (use configured, readonly, edits or auto)", r.Permissions)
	}
	if r.TimeoutSeconds < 0 || time.Duration(r.TimeoutSeconds)*time.Second > MaxTimeout {
		return fmt.Errorf("timeout must be between 0 and %s", MaxTimeout)
	}
	if r.MaxLateSeconds < 0 {
		return errors.New("max-late must not be negative")
	}
	return nil
}

// LogEntry is one recorded run.
type LogEntry struct {
	RoutineID   string    `json:"routine_id"`
	RoutineName string    `json:"routine_name"`
	Trigger     string    `json:"trigger"`
	Slot        time.Time `json:"slot,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Status      string    `json:"status"`
	ExitCode    int       `json:"exit_code"`
	SessionID   string    `json:"session_id,omitempty"`
	Cost        float64   `json:"cost,omitempty"`
	Output      string    `json:"output,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// Duration returns how long the run took.
func (e LogEntry) Duration() time.Duration { return e.FinishedAt.Sub(e.StartedAt) }

type fileData struct {
	Routines []Routine `json:"routines"`
}

// Store persists routines and their run logs under one directory. It is
// safe for concurrent use by several goroutines and several processes.
type Store struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
}

// NewStore returns a store rooted at dir. The directory is created on
// first write.
func NewStore(dir string) *Store {
	return &Store{dir: dir, now: time.Now}
}

// Dir returns the store directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path() string     { return filepath.Join(s.dir, "routines.json") }
func (s *Store) lockPath() string { return filepath.Join(s.dir, "routines.lock") }
func (s *Store) logPath(id string) string {
	return filepath.Join(s.dir, "logs", id+".jsonl")
}

// DaemonLockPath is the file the scheduler loop holds while it runs.
func (s *Store) DaemonLockPath() string { return filepath.Join(s.dir, "daemon.lock") }

// update runs fn under the store lock with the current data and saves the
// result when fn returns nil.
func (s *Store) update(ctx context.Context, fn func(*fileData) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create routine directory: %w", err)
	}
	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, err := lock.File(lockCtx, s.lockPath())
	if err != nil {
		return fmt.Errorf("lock routine store: %w", err)
	}
	defer release()

	data, err := s.load()
	if err != nil {
		return err
	}
	if err := fn(&data); err != nil {
		if errors.Is(err, errNoSave) {
			return nil
		}
		return err
	}
	return s.save(data)
}

func (s *Store) load() (fileData, error) {
	var data fileData
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, fmt.Errorf("read routines: %w", err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return data, fmt.Errorf("parse %s: %w", s.path(), err)
	}
	return data, nil
}

func (s *Store) save(data fileData) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "routines-*.tmp")
	if err != nil {
		return fmt.Errorf("write routines: %w", err)
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write routines: %w", errors.Join(werr, cerr))
	}
	if err := os.Rename(tmpName, s.path()); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace routines: %w", err)
	}
	return nil
}

// List returns every routine, in creation order.
func (s *Store) List(ctx context.Context) ([]Routine, error) {
	var out []Routine
	err := s.update(ctx, func(d *fileData) error {
		out = append([]Routine(nil), d.Routines...)
		return errNoSave
	})
	return out, err
}

// errNoSave lets a read-only callback skip the write.
var errNoSave = errors.New("no save")

func find(d *fileData, key string) int {
	for i, r := range d.Routines {
		if r.ID == key || r.Name == key {
			return i
		}
	}
	for i, r := range d.Routines {
		if len(key) >= 4 && strings.HasPrefix(r.ID, key) {
			return i
		}
	}
	return -1
}

// Get returns a routine by id, unique id prefix or name.
func (s *Store) Get(ctx context.Context, key string) (Routine, error) {
	var out Routine
	err := s.update(ctx, func(d *fileData) error {
		i := find(d, key)
		if i < 0 {
			return fmt.Errorf("%w: %q", ErrNotFound, key)
		}
		out = d.Routines[i]
		return errNoSave
	})
	return out, err
}

// Add stores a new routine and returns it with its id filled in.
func (s *Store) Add(ctx context.Context, r Routine) (Routine, error) {
	if r.Permissions == "" {
		r.Permissions = PermissionsConfigured
	}
	if r.Name == "" {
		r.Name = "routine-" + shortID()
	}
	if err := r.Validate(); err != nil {
		return Routine{}, err
	}
	r.ID = "rt-" + shortID()
	r.CreatedAt = s.now().Truncate(time.Second)
	r.LastSlot, r.LastRunAt, r.LastStatus = time.Time{}, time.Time{}, ""
	r.RunningSince, r.RunningUntil = time.Time{}, time.Time{}
	err := s.update(ctx, func(d *fileData) error {
		if len(d.Routines) >= maxRoutines {
			return fmt.Errorf("at most %d routines", maxRoutines)
		}
		for _, existing := range d.Routines {
			if existing.Name == r.Name {
				return fmt.Errorf("a routine named %q already exists", r.Name)
			}
		}
		d.Routines = append(d.Routines, r)
		return nil
	})
	return r, err
}

// Remove deletes a routine and its log.
func (s *Store) Remove(ctx context.Context, key string) (Routine, error) {
	var removed Routine
	err := s.update(ctx, func(d *fileData) error {
		i := find(d, key)
		if i < 0 {
			return fmt.Errorf("%w: %q", ErrNotFound, key)
		}
		removed = d.Routines[i]
		d.Routines = append(d.Routines[:i], d.Routines[i+1:]...)
		return nil
	})
	if err == nil {
		_ = os.Remove(s.logPath(removed.ID))
	}
	return removed, err
}

// SetEnabled pauses or resumes a routine. Resuming starts counting from
// now, so the time spent paused does not trigger a catch-up run.
func (s *Store) SetEnabled(ctx context.Context, key string, enabled bool) (Routine, error) {
	var out Routine
	err := s.update(ctx, func(d *fileData) error {
		i := find(d, key)
		if i < 0 {
			return fmt.Errorf("%w: %q", ErrNotFound, key)
		}
		r := &d.Routines[i]
		if enabled && !r.Enabled {
			r.LastSlot = s.now().Truncate(time.Minute)
		}
		r.Enabled = enabled
		out = *r
		return nil
	})
	return out, err
}

// Claim marks a routine as running so no other scheduler starts it too. A
// zero slot means a manual run, which does not move the schedule. The
// lease outlives the timeout by a minute and then lapses by itself.
func (s *Store) Claim(ctx context.Context, key string, slot time.Time) (Routine, error) {
	var out Routine
	err := s.update(ctx, func(d *fileData) error {
		i := find(d, key)
		if i < 0 {
			return fmt.Errorf("%w: %q", ErrNotFound, key)
		}
		r := &d.Routines[i]
		now := s.now()
		if r.Running(now) {
			return fmt.Errorf("%w: %s", ErrBusy, r.Name)
		}
		r.RunningSince = now
		r.RunningUntil = now.Add(r.Timeout() + claimGraceSlack)
		if !slot.IsZero() {
			r.LastSlot = slot
		}
		out = *r
		return nil
	})
	return out, err
}

// Finish records a run: it releases the lease, updates the routine's last
// result and appends the entry to its log.
func (s *Store) Finish(ctx context.Context, entry LogEntry) error {
	entry.Output = truncateOutput(entry.Output)
	err := s.update(ctx, func(d *fileData) error {
		i := find(d, entry.RoutineID)
		if i < 0 {
			// Removed while it ran; there is nothing left to update.
			return errNoSave
		}
		r := &d.Routines[i]
		r.RunningSince, r.RunningUntil = time.Time{}, time.Time{}
		r.LastRunAt = entry.StartedAt
		r.LastStatus = entry.Status
		return nil
	})
	if err != nil {
		return err
	}
	return s.appendLog(entry)
}

// Logs returns up to limit of the newest entries for a routine, newest
// first. A limit of zero or less returns all kept entries.
func (s *Store) Logs(ctx context.Context, key string, limit int) ([]LogEntry, error) {
	r, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	entries, err := s.readLog(r.ID)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if limit <= 0 || limit > maxLogEntries {
		limit = maxLogEntries
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (s *Store) appendLog(entry LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.logPath(entry.RoutineID)), 0o700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	release, err := lock.File(context.Background(), s.lockPath())
	if err != nil {
		return fmt.Errorf("lock routine store: %w", err)
	}
	defer release()

	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	path := s.logPath(entry.RoutineID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open routine log: %w", err)
	}
	_, werr := f.Write(append(line, '\n'))
	size := int64(0)
	if info, serr := f.Stat(); serr == nil {
		size = info.Size()
	}
	if err := errors.Join(werr, f.Close()); err != nil {
		return fmt.Errorf("write routine log: %w", err)
	}
	if size > logCompactBytes {
		return s.compactLog(path, entry.RoutineID)
	}
	return nil
}

// compactLog rewrites a log that has grown past logCompactBytes, keeping
// the newest entries within the entry and size budgets. Callers hold the
// store lock.
func (s *Store) compactLog(path, id string) error {
	entries, err := s.readLog(id)
	if err != nil {
		return err
	}
	var kept []LogEntry
	total := 0
	for i := len(entries) - 1; i >= 0 && len(kept) < maxLogEntries; i-- {
		size := len(entries[i].Output) + 512
		if total+size > logKeepBytes && len(kept) > 0 {
			break
		}
		total += size
		kept = append(kept, entries[i])
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	for i := len(kept) - 1; i >= 0; i-- {
		if err := enc.Encode(kept[i]); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(buf.String()), 0o600); err != nil {
		return fmt.Errorf("write routine log: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace routine log: %w", err)
	}
	return nil
}

func (s *Store) readLog(id string) ([]LogEntry, error) {
	f, err := os.Open(s.logPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read routine log: %w", err)
	}
	defer f.Close()
	var entries []LogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*maxOutputBytes)
	for sc.Scan() {
		var e LogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}

// truncateOutput keeps the tail of long output, where a run's conclusion
// usually is.
func truncateOutput(out string) string {
	if len(out) <= maxOutputBytes {
		return out
	}
	tail := out[len(out)-maxOutputBytes:]
	// Do not start in the middle of a UTF-8 sequence.
	for i := 0; i < len(tail) && i < 4; i++ {
		if tail[i]&0xC0 != 0x80 {
			tail = tail[i:]
			break
		}
	}
	return "[output truncated]\n" + tail
}

func shortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b)
}
