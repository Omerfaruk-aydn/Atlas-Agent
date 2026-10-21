package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"mvdan.cc/sh/v3/syntax"
)

const (
	// MaxBackgroundJobs is the maximum number of concurrent background jobs allowed
	MaxBackgroundJobs = 50
	// CompletedJobRetentionMinutes is how long to keep completed jobs before auto-cleanup (8 hours)
	CompletedJobRetentionMinutes = 8 * 60
)

// syncBuffer is a thread-safe wrapper around bytes.Buffer.
type syncBuffer struct {
	buf bytes.Buffer
	mu  sync.RWMutex
	// limit, when positive, keeps only roughly the last limit bytes so a
	// long-running job cannot grow memory without bound. total counts
	// every byte written.
	limit int
	total int64
}

func (sb *syncBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.total += int64(len(p))
	n, err = sb.buf.Write(p)
	if sb.limit > 0 && sb.buf.Len() > 2*sb.limit {
		keep := bytes.Clone(sb.buf.Bytes()[sb.buf.Len()-sb.limit:])
		sb.buf.Reset()
		sb.buf.Write(keep)
	}
	return n, err
}

// Total reports how many bytes were written, including discarded ones.
func (sb *syncBuffer) Total() int64 {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.total
}

func (sb *syncBuffer) WriteString(s string) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.WriteString(s)
}

func (sb *syncBuffer) String() string {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.buf.String()
}

// BackgroundShell represents a shell running in the background.
type BackgroundShell struct {
	ID          string
	Command     string
	Description string
	Shell       *Shell
	WorkingDir  string
	StartedAt   time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	stdout      *syncBuffer
	stderr      *syncBuffer
	done        chan struct{}
	exitErr     error
	completedAt atomic.Int64 // Unix timestamp when job completed (0 if still running)
	timedOut    atomic.Bool
}

// BackgroundShellManager manages background shell instances.
type BackgroundShellManager struct {
	*pubsub.Broker[JobEvent]
	shells *csync.Map[string, *BackgroundShell]
}

var (
	backgroundManager     *BackgroundShellManager
	backgroundManagerOnce sync.Once
	idCounter             atomic.Uint64
)

// newBackgroundShellManager creates a new BackgroundShellManager instance.
func newBackgroundShellManager() *BackgroundShellManager {
	return &BackgroundShellManager{
		Broker: pubsub.NewBroker[JobEvent](),
		shells: csync.NewMap[string, *BackgroundShell](),
	}
}

// GetBackgroundShellManager returns the singleton background shell manager.
func GetBackgroundShellManager() *BackgroundShellManager {
	backgroundManagerOnce.Do(func() {
		backgroundManager = newBackgroundShellManager()
	})
	return backgroundManager
}

// Start creates and starts a new background shell with the given command.
func (m *BackgroundShellManager) Start(ctx context.Context, workingDir string, blockFuncs []BlockFunc, command string, description string) (*BackgroundShell, error) {
	return m.start(ctx, workingDir, blockFuncs, command, description, 0, 0, func(ctx context.Context, sh *Shell, stdout, stderr io.Writer) error {
		return sh.ExecStream(ctx, command, stdout, stderr)
	})
}

// ArgvOutputLimit bounds the output a literal argv job keeps per stream.
const ArgvOutputLimit = 1 << 20

// StartArgv starts literal argv as a background job. No shell parses the
// arguments. A positive timeout ends the job when it expires; the job then
// reports TimedOut. Output kept per stream is bounded by ArgvOutputLimit.
func (m *BackgroundShellManager) StartArgv(ctx context.Context, workingDir string, blockFuncs []BlockFunc, argv []string, description string, timeout time.Duration) (*BackgroundShell, error) {
	if err := ValidateArgv(argv); err != nil {
		return nil, err
	}
	if BlockedArgv(blockFuncs, argv) {
		return nil, fmt.Errorf("command blocked by policy")
	}
	return m.start(ctx, workingDir, blockFuncs, QuoteArgv(argv), description, timeout, ArgvOutputLimit, func(ctx context.Context, sh *Shell, stdout, stderr io.Writer) error {
		return sh.ExecArgvStream(ctx, argv, stdout, stderr)
	})
}

// QuoteArgv renders argv for display only; it is never executed.
func QuoteArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		quoted, err := syntax.Quote(arg, syntax.LangBash)
		if err != nil {
			quoted = strconv.Quote(arg)
		}
		parts[i] = quoted
	}
	return strings.Join(parts, " ")
}

func (m *BackgroundShellManager) start(ctx context.Context, workingDir string, blockFuncs []BlockFunc, command, description string, timeout time.Duration, limit int, run func(context.Context, *Shell, io.Writer, io.Writer) error) (*BackgroundShell, error) {
	// Check job limit
	if m.shells.Len() >= MaxBackgroundJobs {
		return nil, fmt.Errorf("maximum number of background jobs (%d) reached. Please terminate or wait for some jobs to complete", MaxBackgroundJobs)
	}

	id := fmt.Sprintf("%03X", idCounter.Add(1))

	shell := NewShell(&Options{
		WorkingDir: workingDir,
		BlockFuncs: blockFuncs,
	})

	shellCtx, cancel := context.WithCancel(ctx)
	if timeout > 0 {
		var cancelTimeout context.CancelFunc
		shellCtx, cancelTimeout = context.WithTimeout(shellCtx, timeout)
		cancelParent := cancel
		cancel = func() { cancelTimeout(); cancelParent() }
	}

	bgShell := &BackgroundShell{
		ID:          id,
		Command:     command,
		Description: description,
		WorkingDir:  workingDir,
		StartedAt:   time.Now(),
		Shell:       shell,
		ctx:         shellCtx,
		cancel:      cancel,
		stdout:      &syncBuffer{limit: limit},
		stderr:      &syncBuffer{limit: limit},
		done:        make(chan struct{}),
	}

	m.shells.Set(id, bgShell)
	m.Publish(pubsub.UpdatedEvent, JobEvent{Type: JobEventStarted, Info: bgShell.info()})

	go func() {
		err := run(shellCtx, shell, bgShell.stdout, bgShell.stderr)
		if timeout > 0 && errors.Is(shellCtx.Err(), context.DeadlineExceeded) {
			bgShell.timedOut.Store(true)
		}

		bgShell.exitErr = err
		bgShell.completedAt.Store(time.Now().Unix())
		// Close done (which IsDone/info rely on) before publishing, so the
		// completion event's snapshot correctly reports Done/Status instead
		// of a still-running snapshot racing the channel close.
		close(bgShell.done)
		m.Publish(pubsub.UpdatedEvent, JobEvent{Type: JobEventCompleted, Info: bgShell.info()})
	}()

	return bgShell, nil
}

// Get retrieves a background shell by ID.
func (m *BackgroundShellManager) Get(id string) (*BackgroundShell, bool) {
	return m.shells.Get(id)
}

// Remove removes a background shell from the manager without terminating it.
// This is useful when a shell has already completed and you just want to clean up tracking.
func (m *BackgroundShellManager) Remove(id string) error {
	_, ok := m.shells.Take(id)
	if !ok {
		return fmt.Errorf("background shell not found: %s", id)
	}
	return nil
}

// killWait bounds how long Kill waits for a canceled shell to finish.
const killWait = 10 * time.Second

// ErrStillStopping reports that a killed job had not finished within the
// wait. It was told to stop and is no longer tracked; only its exit is
// unconfirmed.
var ErrStillStopping = errors.New("the job was told to stop but has not exited yet")

// Kill terminates a background shell by ID and waits, at most killWait,
// for it to finish.
func (m *BackgroundShellManager) Kill(id string) error {
	return m.KillContext(context.Background(), id)
}

// KillContext is Kill whose wait also ends when ctx does, so a caller that
// is itself canceled (a turn the user stopped) is never held by a job that
// will not exit.
func (m *BackgroundShellManager) KillContext(ctx context.Context, id string) error {
	shell, ok := m.shells.Take(id)
	if !ok {
		return fmt.Errorf("background shell not found: %s", id)
	}

	shell.cancel()
	timer := time.NewTimer(killWait)
	defer timer.Stop()
	select {
	case <-shell.done:
		return nil
	case <-timer.C:
		return ErrStillStopping
	case <-ctx.Done():
		return ctx.Err()
	}
}

// JobStatus is the current lifecycle state of a background shell.
type JobStatus string

const (
	JobStatusRunning JobStatus = "running"
	JobStatusDone    JobStatus = "done"
	JobStatusFailed  JobStatus = "failed"
	JobStatusKilled  JobStatus = "killed"
)

// BackgroundShellInfo is a point-in-time snapshot of a background shell,
// safe to pass around without exposing the shell's internal output buffers.
type BackgroundShellInfo struct {
	ID          string
	Command     string
	Description string
	WorkingDir  string
	StartedAt   time.Time
	Done        bool
	Status      JobStatus
	ExitErr     string
}

// info builds the current point-in-time snapshot for bs.
func (bs *BackgroundShell) info() BackgroundShellInfo {
	done := bs.IsDone()
	status := JobStatusRunning
	exitErr := ""
	if done {
		switch {
		case bs.exitErr == nil:
			status = JobStatusDone
		case bs.ctx.Err() != nil:
			// The shell's own context was canceled (Kill), rather than the
			// command failing on its own — report it as killed instead of
			// failed even though ExecStream also returns a non-nil error
			// in that case.
			status = JobStatusKilled
		default:
			status = JobStatusFailed
			exitErr = bs.exitErr.Error()
		}
	}
	return BackgroundShellInfo{
		ID:          bs.ID,
		Command:     bs.Command,
		Description: bs.Description,
		WorkingDir:  bs.WorkingDir,
		StartedAt:   bs.StartedAt,
		Done:        done,
		Status:      status,
		ExitErr:     exitErr,
	}
}

// List returns all background shell IDs.
func (m *BackgroundShellManager) List() []string {
	ids := make([]string, 0, m.shells.Len())
	for id := range m.shells.Seq2() {
		ids = append(ids, id)
	}
	return ids
}

// ListInfo returns a point-in-time snapshot of every tracked background
// shell, running or completed (until Cleanup evicts it).
func (m *BackgroundShellManager) ListInfo() []BackgroundShellInfo {
	infos := make([]BackgroundShellInfo, 0, m.shells.Len())
	for shell := range m.shells.Seq() {
		infos = append(infos, shell.info())
	}
	return infos
}

// Cleanup removes completed jobs that have been finished for more than the retention period
func (m *BackgroundShellManager) Cleanup() int {
	now := time.Now().Unix()
	retentionSeconds := int64(CompletedJobRetentionMinutes * 60)

	var toRemove []string
	for shell := range m.shells.Seq() {
		completedAt := shell.completedAt.Load()
		if completedAt > 0 && now-completedAt > retentionSeconds {
			toRemove = append(toRemove, shell.ID)
		}
	}

	for _, id := range toRemove {
		m.Remove(id)
	}

	return len(toRemove)
}

// KillAll terminates all background shells. The provided context bounds how
// long the function waits for each shell to exit.
func (m *BackgroundShellManager) KillAll(ctx context.Context) {
	shells := slices.Collect(m.shells.Seq())
	m.shells.Reset(map[string]*BackgroundShell{})

	var wg sync.WaitGroup
	for _, shell := range shells {
		wg.Go(func() {
			shell.cancel()
			select {
			case <-shell.done:
			case <-ctx.Done():
			}
		})
	}
	wg.Wait()
}

// GetOutput returns the current output of a background shell.
func (bs *BackgroundShell) GetOutput() (stdout string, stderr string, done bool, err error) {
	select {
	case <-bs.done:
		return bs.stdout.String(), bs.stderr.String(), true, bs.exitErr
	default:
		return bs.stdout.String(), bs.stderr.String(), false, nil
	}
}

// TimedOut reports that the job's own time limit, not a kill or the
// command itself, ended it.
func (bs *BackgroundShell) TimedOut() bool { return bs.timedOut.Load() }

// OutputBytes reports how many bytes each stream produced in total,
// including bytes no longer retained.
func (bs *BackgroundShell) OutputBytes() (stdout, stderr int64) {
	return bs.stdout.Total(), bs.stderr.Total()
}

// IsDone checks if the background shell has finished execution.
func (bs *BackgroundShell) IsDone() bool {
	select {
	case <-bs.done:
		return true
	default:
		return false
	}
}

// Wait blocks until the background shell completes.
func (bs *BackgroundShell) Wait() {
	<-bs.done
}

func (bs *BackgroundShell) WaitContext(ctx context.Context) bool {
	select {
	case <-bs.done:
		return true
	case <-ctx.Done():
		return false
	}
}
