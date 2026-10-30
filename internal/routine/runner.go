package routine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/appenv"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
)

// Result is what an [Executor] reports for one run.
type Result struct {
	Output    string
	SessionID string
	Cost      float64
}

// Executor runs one routine to completion. It must stop when ctx ends.
type Executor interface {
	Execute(ctx context.Context, r Routine) (Result, error)
}

// ExecutorFunc adapts a function to an [Executor].
type ExecutorFunc func(ctx context.Context, r Routine) (Result, error)

// Execute calls f.
func (f ExecutorFunc) Execute(ctx context.Context, r Routine) (Result, error) { return f(ctx, r) }

// Runner executes due routines through an [Executor] and records the
// results in a [Store].
type Runner struct {
	Store *Store
	Exec  Executor
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Concurrency caps simultaneous runs; 2 when zero.
	Concurrency int

	semOnce sync.Once
	sem     chan struct{}
}

func (rn *Runner) now() time.Time {
	if rn.Now != nil {
		return rn.Now()
	}
	return time.Now()
}

func (rn *Runner) concurrency() int {
	if rn.Concurrency > 0 {
		return rn.Concurrency
	}
	return 2
}

// pending is a claimed run waiting for a worker.
type pending struct {
	routine Routine
	slot    time.Time
	trigger string
}

// RunDue runs every routine that is due now and returns the entries
// recorded. A routine that is already running elsewhere is left alone.
func (rn *Runner) RunDue(ctx context.Context) ([]LogEntry, error) {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		entries []LogEntry
	)
	err := rn.dispatch(ctx, &wg, func(e LogEntry) {
		mu.Lock()
		entries = append(entries, e)
		mu.Unlock()
	})
	wg.Wait()
	return entries, err
}

// RunOne runs a single routine immediately, whether or not it is due or
// enabled. It does not move the routine's schedule.
func (rn *Runner) RunOne(ctx context.Context, key string) (LogEntry, error) {
	r, err := rn.Store.Claim(ctx, key, time.Time{})
	if err != nil {
		return LogEntry{}, err
	}
	return rn.execute(ctx, pending{routine: r, trigger: TriggerManual}), nil
}

// dispatch claims every due routine and starts a worker for each, bounded
// by the concurrency limit. Claiming happens before it returns so a later
// call cannot start the same occurrence again.
func (rn *Runner) dispatch(ctx context.Context, wg *sync.WaitGroup, sink func(LogEntry)) error {
	routines, err := rn.Store.List(ctx)
	if err != nil {
		return err
	}
	now := rn.now()
	sem := rn.semaphore()
	var errs []error
	for _, r := range routines {
		slot, due := r.DueSlot(now)
		if !due || r.Running(now) {
			continue
		}
		claimed, err := rn.Store.Claim(ctx, r.ID, slot)
		if errors.Is(err, ErrBusy) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p := pending{routine: claimed, slot: slot, trigger: TriggerSchedule}
		if claimed.MaxLateSeconds > 0 && now.Sub(slot) > time.Duration(claimed.MaxLateSeconds)*time.Second {
			sink(rn.skip(ctx, p, now))
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				sink(rn.finishUnstarted(p, ctx.Err()))
				return
			}
			sink(rn.execute(ctx, p))
		}()
	}
	return errors.Join(errs...)
}

// semaphore returns the worker limiter, created on first use so the limit
// holds across scheduler passes.
func (rn *Runner) semaphore() chan struct{} {
	rn.semOnce.Do(func() { rn.sem = make(chan struct{}, rn.concurrency()) })
	return rn.sem
}

func (rn *Runner) skip(ctx context.Context, p pending, now time.Time) LogEntry {
	e := LogEntry{
		RoutineID: p.routine.ID, RoutineName: p.routine.Name, Trigger: p.trigger,
		Slot: p.slot, StartedAt: now, FinishedAt: now, Status: StatusSkipped,
		Error: fmt.Sprintf("occurrence for %s was more than %ds late", p.slot.Format(time.RFC3339), p.routine.MaxLateSeconds),
	}
	rn.record(ctx, e)
	return e
}

func (rn *Runner) finishUnstarted(p pending, cause error) LogEntry {
	now := rn.now()
	e := LogEntry{
		RoutineID: p.routine.ID, RoutineName: p.routine.Name, Trigger: p.trigger,
		Slot: p.slot, StartedAt: now, FinishedAt: now, Status: StatusSkipped,
		Error: fmt.Sprintf("scheduler stopped before the run started: %v", cause),
	}
	rn.record(context.Background(), e)
	return e
}

// record stores an entry even when ctx has been cancelled, so an
// interrupted run still leaves a trace.
func (rn *Runner) record(ctx context.Context, e LogEntry) {
	if err := rn.Store.Finish(context.WithoutCancel(ctx), e); err != nil {
		slog.Error("Failed to record routine run", "routine", e.RoutineName, "error", err)
	}
}

func (rn *Runner) execute(ctx context.Context, p pending) LogEntry {
	r := p.routine
	started := rn.now()
	runCtx, cancel := context.WithTimeout(ctx, r.Timeout())
	defer cancel()

	res, err := rn.Exec.Execute(runCtx, r)
	e := LogEntry{
		RoutineID: r.ID, RoutineName: r.Name, Trigger: p.trigger, Slot: p.slot,
		StartedAt: started, FinishedAt: rn.now(), Status: StatusSucceeded,
		SessionID: res.SessionID, Cost: res.Cost, Output: res.Output,
	}
	switch {
	case err == nil:
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		e.Status = StatusTimedOut
		e.Error = fmt.Sprintf("timed out after %s", r.Timeout())
	default:
		e.Status = StatusFailed
		e.Error = err.Error()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			e.ExitCode = exit.ExitCode()
		} else {
			e.ExitCode = -1
		}
	}
	rn.record(ctx, e)
	return e
}

// DaemonOptions configures [Runner.Daemon].
type DaemonOptions struct {
	// Interval is how often the scheduler looks for due routines; 30
	// seconds when zero.
	Interval time.Duration
	// OnRun, when set, is called with every recorded run.
	OnRun func(LogEntry)
}

// ErrDaemonRunning reports that another scheduler loop holds the lock.
var ErrDaemonRunning = errors.New("another routine daemon is already running")

// Daemon runs the scheduler loop until ctx ends. Only one daemon per store
// runs at a time. When ctx ends it waits for runs in flight, which are
// cancelled through the same context.
func (rn *Runner) Daemon(ctx context.Context, opts DaemonOptions) error {
	if err := os.MkdirAll(rn.Store.Dir(), 0o700); err != nil {
		return err
	}
	release, err := lock.TryFile(rn.Store.DaemonLockPath())
	if errors.Is(err, lock.ErrContended) {
		return ErrDaemonRunning
	}
	if err != nil {
		return err
	}
	defer release()

	interval := opts.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	sink := func(e LogEntry) {
		if opts.OnRun != nil {
			opts.OnRun(e)
		}
	}

	tick := func() {
		if err := rn.dispatch(ctx, &wg, sink); err != nil {
			slog.Error("Routine scheduler pass failed", "error", err)
		}
	}
	tick()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick()
		}
	}
}

// ProcessExecutor runs a routine by starting the agent binary's
// non-interactive "run" command in the routine's directory. The prompt
// travels on stdin so its length and quoting never matter.
type ProcessExecutor struct {
	// Binary is the agent executable; the running executable when empty.
	Binary string
	// ExtraArgs are global flags placed before "run", such as --data-dir.
	ExtraArgs []string
}

// Args builds the command line for r, without the binary.
func (e ProcessExecutor) Args(r Routine) []string {
	args := []string{"--cwd", r.Dir}
	args = append(args, e.ExtraArgs...)
	args = append(args, "run", "--json", "--quiet", "--permissions", r.Permissions)
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	if r.Goal {
		args = append(args, "--goal")
	}
	return args
}

// Execute implements [Executor].
func (e ProcessExecutor) Execute(ctx context.Context, r Routine) (Result, error) {
	binary := e.Binary
	if binary == "" {
		exe, err := os.Executable()
		if err != nil {
			return Result{}, fmt.Errorf("find agent executable: %w", err)
		}
		binary = exe
	}
	if info, err := os.Stat(r.Dir); err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("working directory %q is not available", r.Dir)
	}
	cmd := exec.CommandContext(ctx, binary, e.Args(r)...)
	cmd.Dir = r.Dir
	cmd.Stdin = strings.NewReader(r.Prompt)
	// The child must run the agent in-process: the permission policy and
	// the JSON result are only honored there.
	cmd.Env = append(os.Environ(), appenv.Prefix+"CLIENT_SERVER=0", appenv.Prefix+"ROUTINE="+r.ID)
	cmd.WaitDelay = 10 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: 16 * 1024}
	runErr := cmd.Run()

	res := parseRunOutput(stdout.Bytes())
	if runErr != nil {
		if res.Output == "" {
			res.Output = strings.TrimSpace(stderr.String())
		}
		if tail := lastLines(stderr.String(), 8); tail != "" {
			return res, fmt.Errorf("%w: %s", runErr, tail)
		}
		return res, runErr
	}
	return res, nil
}

// parseRunOutput reads the document "run --json" prints. Anything else is
// kept as plain output.
func parseRunOutput(stdout []byte) Result {
	trimmed := bytes.TrimSpace(stdout)
	var doc struct {
		SessionID string  `json:"session_id"`
		Response  string  `json:"response"`
		Cost      float64 `json:"cost"`
	}
	if err := json.Unmarshal(trimmed, &doc); err == nil && (doc.SessionID != "" || doc.Response != "") {
		return Result{Output: doc.Response, SessionID: doc.SessionID, Cost: doc.Cost}
	}
	return Result{Output: string(trimmed)}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// limitedBuffer keeps only the last max bytes written to it.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	l.buf.Write(p)
	if over := l.buf.Len() - l.max; over > 0 {
		l.buf.Next(over)
	}
	return len(p), nil
}
