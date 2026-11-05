//go:build !windows

package procown

import (
	"os/exec"
	"syscall"
	"time"
)

type groupHandle = int

func prepare(cmd *exec.Cmd, _ bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attach(cmd *exec.Cmd) (groupHandle, error) { return cmd.Process.Pid, nil }

func killGroup(cmd *exec.Cmd, pgid groupHandle) {
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		go func() {
			time.Sleep(2 * time.Second)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}()
		return
	}
	_ = cmd.Process.Kill()
}

func closeGroup(groupHandle) {}

func killPID(pid int) error {
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		return syscall.Kill(-pid, syscall.SIGKILL)
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
