// Package procown starts processes Atlas owns and identifies them later by
// PID plus OS creation time, so a recycled PID that now belongs to a user's
// own program is never signalled. Owned processes are started in their own
// process group (Unix) or job object (Windows) so the whole tree can be
// stopped.
package procown

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// Identity names one process instance.
type Identity struct {
	PID int `json:"pid"`
	// CreatedAt is the OS-reported creation time in Unix milliseconds, or
	// zero when the platform cannot report it.
	CreatedAt int64 `json:"created_at"`
}

// Spec describes a process to start. Argv is literal; no shell is involved.
type Spec struct {
	Argv   []string
	Dir    string
	Env    []string
	Stdout io.Writer
	Stderr io.Writer
	// Visible shows the program's window (GUI launches). Console helpers
	// stay hidden by default.
	Visible bool
}

// Proc is a running owned process.
type Proc struct {
	Identity Identity
	cmd      *exec.Cmd
	group    groupHandle
	done     chan struct{}
	mu       sync.Mutex
	err      error
	exit     int
}

// ErrNotOwned is returned when an identity no longer matches a live process.
var ErrNotOwned = errors.New("process identity does not match a live owned process")

// Start launches spec as an owned process.
func Start(spec Spec) (*Proc, error) {
	if len(spec.Argv) == 0 || spec.Argv[0] == "" {
		return nil, fmt.Errorf("empty argv")
	}
	// Lifetime is managed by Stop and the job object, never by a context.
	cmd := exec.CommandContext(context.Background(), spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	cmd.Stdout, cmd.Stderr = spec.Stdout, spec.Stderr
	prepare(cmd, spec.Visible)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	group, err := attach(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("own process tree: %w", err)
	}
	created, _ := CreationTime(cmd.Process.Pid)
	p := &Proc{Identity: Identity{PID: cmd.Process.Pid, CreatedAt: created}, cmd: cmd, group: group, done: make(chan struct{}), exit: -1}
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.err = err
		if cmd.ProcessState != nil {
			p.exit = cmd.ProcessState.ExitCode()
		}
		p.mu.Unlock()
		close(p.done)
	}()
	return p, nil
}

// Done is closed when the process exits.
func (p *Proc) Done() <-chan struct{} { return p.done }

// Result returns the exit code (-1 if unknown) and wait error after Done.
func (p *Proc) Result() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exit, p.err
}

// Wait blocks until exit or ctx ends. On ctx end it stops the tree.
func (p *Proc) Wait(ctx context.Context) (int, error) {
	select {
	case <-p.done:
		return p.Result()
	case <-ctx.Done():
		p.Stop(3 * time.Second)
		<-p.done
		code, _ := p.Result()
		return code, ctx.Err()
	}
}

// Stop terminates the process tree, waiting up to grace for exit.
func (p *Proc) Stop(grace time.Duration) {
	select {
	case <-p.done:
		closeGroup(p.group)
		return
	default:
	}
	killGroup(p.cmd, p.group)
	select {
	case <-p.done:
	case <-time.After(grace):
		_ = p.cmd.Process.Kill()
	}
	closeGroup(p.group)
}

// Matches reports whether id still names the same live process instance.
// A zero CreatedAt never matches, because PID alone is not ownership.
func Matches(id Identity) bool {
	if id.PID <= 0 || id.CreatedAt == 0 {
		return false
	}
	created, err := CreationTime(id.PID)
	if err != nil || created == 0 {
		return false
	}
	// Allow small rounding differences between APIs.
	d := created - id.CreatedAt
	return d >= -2 && d <= 2
}

// KillOwned terminates the process tree named by id only when the identity
// still matches. It never signals a process whose creation time differs.
func KillOwned(id Identity) error {
	if !Matches(id) {
		return ErrNotOwned
	}
	return killPID(id.PID)
}
