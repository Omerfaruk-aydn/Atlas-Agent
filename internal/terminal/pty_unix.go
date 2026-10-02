//go:build linux

package terminal

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type unixStream struct{ *os.File }

func (s unixStream) Read(data []byte) (int, error) {
	n, err := s.File.Read(data)
	// Linux returns EIO at a PTY slave's final close, not a pipe EOF.
	if errors.Is(err, syscall.EIO) {
		err = io.EOF
	}
	return n, err
}

func startProcess(req execution.Request, size execution.TerminalSize) (processTerminal, error) {
	executable := req.Argv[0]
	if strings.ContainsRune(executable, '/') && !filepath.IsAbs(executable) {
		executable = filepath.Join(req.Root, executable)
	}
	command := exec.Command(executable, req.Argv[1:]...)
	command.Dir, command.Env = req.Root, req.Env
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: uint16(size.Width), Rows: uint16(size.Height)})
	if err != nil {
		return processTerminal{}, err
	}
	var processMu sync.Mutex
	finished := false
	return processTerminal{
		stream: unixStream{terminal},
		resize: func(size execution.TerminalSize) error {
			return pty.Setsize(terminal, &pty.Winsize{Cols: uint16(size.Width), Rows: uint16(size.Height)})
		},
		kill: func() error {
			processMu.Lock()
			defer processMu.Unlock()
			if finished {
				return nil
			}
			err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			if errors.Is(err, syscall.ESRCH) {
				return nil
			}
			return err
		},
		wait: func() (int, error) {
			// Observe without reaping: the owned primary PID cannot be reused
			// while its process group and descendants are terminated.
			var observed unix.Siginfo
			observeErr := unix.Waitid(unix.P_PID, command.Process.Pid, &observed, unix.WEXITED|unix.WNOWAIT, nil)
			processMu.Lock()
			killErr := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			if errors.Is(killErr, syscall.ESRCH) {
				killErr = nil
			}
			err := command.Wait()
			finished = true
			processMu.Unlock()
			if observeErr != nil || killErr != nil {
				return -1, errors.Join(observeErr, killErr, err)
			}
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return exit.ExitCode(), nil
			}
			if err != nil {
				return -1, err
			}
			return 0, nil
		},
		finish: func() {
			// The slave closes with its last process. Do not close the master
			// before the collector has drained its final bytes.
		},
	}, nil
}
