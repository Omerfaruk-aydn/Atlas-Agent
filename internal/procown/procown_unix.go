//go:build !windows

package procown

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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

// CreationTime returns the process start time in Unix milliseconds. It is
// implemented for Linux via /proc; other Unix systems report an error, which
// makes ownership checks fail closed.
func CreationTime(pid int) (int64, error) {
	if runtime.GOOS != "linux" {
		return 0, errors.New("process creation time unavailable on " + runtime.GOOS)
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	// Field 22 (starttime) follows the parenthesised command name.
	text := string(data)
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return 0, errors.New("malformed stat")
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) < 20 {
		return 0, errors.New("malformed stat")
	}
	if fields[0] == "Z" {
		return 0, ErrNotOwned
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, err
	}
	boot, err := bootTime()
	if err != nil {
		return 0, err
	}
	const hz = 100 // USER_HZ is 100 on every supported Linux ABI.
	return boot*1000 + ticks*1000/hz, nil
}

func bootTime() (int64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
	}
	return 0, errors.New("btime missing")
}

func killPID(pid int) error {
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		return syscall.Kill(-pid, syscall.SIGKILL)
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
