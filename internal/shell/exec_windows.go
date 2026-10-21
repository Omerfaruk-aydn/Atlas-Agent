//go:build windows

package shell

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/sandbox"
	"golang.org/x/sys/windows"
	"mvdan.cc/sh/v3/interp"
)

// defaultKillTimeout matches mvdan's DefaultExecHandler default.
const defaultKillTimeout = 2 * time.Second

// pipeWaitDelay bounds how long a canceled command waits for its output
// pipes once the process itself is gone. A grandchild that escaped the
// tree kill (or one taskkill could not reach) keeps the pipes open, and
// without a bound cmd.Wait -- and with it a job_kill or a turn's Stop --
// would block until that grandchild exits, which for a dev server is
// never.
const pipeWaitDelay = 3 * time.Second

// isolateProcess is a no-op on Windows. Session isolation via Setsid is a
// Unix-only concept; Windows uses CREATE_NEW_PROCESS_GROUP which mvdan's
// default handler already handles adequately.
func isolateProcess(_ *exec.Cmd) {}

// processGroupExecHandler mirrors interp.DefaultExecHandler on Windows
// with two additions: canceling a command ends its whole process tree, not
// just the direct child, and the wait for its output pipes is bounded.
// When sandboxing has been turned on via SetSandboxLimits every process it
// spawns is additionally assigned to a sandbox.Job.
func processGroupExecHandler(_ time.Duration) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		limits, enabled := currentSandboxLimits()
		return windowsExec(ctx, args, limits, enabled)
	}
}

// windowsExec runs one command for the interpreter: same lookup, same
// immediate-kill-on-cancel semantics and same exit-status mapping as
// interp.DefaultExecHandler.
//
// With sandboxed set, the spawned process is assigned to a fresh
// sandbox.Job right after it starts, so it (and anything it spawns) cannot
// outlive the job closing, and is optionally capped on process count /
// memory. See internal/sandbox's package doc for exactly what that does and
// does not protect against. A job that fails to create or assign falls back
// to running the command unsandboxed rather than refusing to run it at all
// -- logged so the gap is visible, since silently degrading a safety
// feature is worse than a loud one.
func windowsExec(ctx context.Context, args []string, limits sandbox.Limits, sandboxed bool) error {
	hc := interp.HandlerCtx(ctx)
	path, err := interp.LookPathDir(hc.Dir, hc.Env, args[0])
	if err != nil {
		fmt.Fprintln(hc.Stderr, err)
		return interp.ExitStatus(127)
	}

	var job *sandbox.Job
	if sandboxed {
		var jobErr error
		job, jobErr = sandbox.New(limits)
		if jobErr != nil {
			slog.Warn("Failed to create sandbox job; running command unsandboxed", "command", args[0], "error", jobErr)
		}
	}
	// Cancellation may close the job early; the handle must be closed once.
	closeJob := sync.OnceFunc(func() { _ = job.Close() })
	defer closeJob()

	cmd := exec.Cmd{
		Path:      path,
		Args:      args,
		Env:       execEnvList(hc.Env),
		Dir:       hc.Dir,
		Stdin:     hc.Stdin,
		Stdout:    hc.Stdout,
		Stderr:    hc.Stderr,
		WaitDelay: pipeWaitDelay,
	}

	err = cmd.Start()
	if err == nil {
		if job != nil {
			if assignErr := job.Assign(cmd.Process); assignErr != nil {
				slog.Warn("Failed to sandbox process; it will run unconstrained", "command", args[0], "error", assignErr)
			}
		}

		stopf := context.AfterFunc(ctx, func() {
			// Go doesn't support sending Interrupt on Windows, so
			// cancellation kills immediately, as interp.DefaultExecHandler
			// does. The tree goes first: once the direct child is gone its
			// children can no longer be found from its PID.
			killProcessTree(cmd.Process.Pid)
			_ = cmd.Process.Signal(os.Kill)
			// Closing the job ends anything assigned to it that taskkill
			// missed.
			closeJob()
		})
		defer stopf()

		err = cmd.Wait()
	}

	switch err := err.(type) {
	case *exec.ExitError:
		if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return interp.ExitStatus(128 + uint8(status.Signal()))
		}
		return interp.ExitStatus(uint8(err.ExitCode()))
	case *exec.Error:
		fmt.Fprintf(hc.Stderr, "%v\n", err)
		return interp.ExitStatus(127)
	default:
		if errors.Is(err, exec.ErrWaitDelay) {
			// The process exited; only an escaped grandchild still held
			// the output open.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		return err
	}
}

// killProcessTree ends pid and every process it started. Windows has no
// process groups a signal could reach, so it asks taskkill, without a
// console window, and gives it a few seconds at most.
func killProcessTree(pid int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kill := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if err := kill.Run(); err != nil {
		slog.Debug("Failed to end the process tree", "pid", pid, "error", err)
	}
}
