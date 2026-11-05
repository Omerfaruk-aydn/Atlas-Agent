//go:build !windows

package dots

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

func hideWindow(cmd *exec.Cmd) {
	// The browser and the helpers it spawns stay in one group, so Stop
	// can signal all of them. Chrome also reparents some helpers; those
	// are found from /proc when the group signal does not reach them.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	killDescendants(pid)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	err = p.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func screenExited(pid int) bool {
	if pid <= 0 {
		return true
	}
	return !processAlive(pid) && !hasDescendants(pid)
}

func hasDescendants(pid int) bool {
	return len(childPIDs(pid)) > 0
}

func killDescendants(pid int) {
	var pending []int
	pending = append(pending, childPIDs(pid)...)
	for i := 0; i < len(pending); i++ {
		id := pending[i]
		pending = append(pending, childPIDs(id)...)
		_ = syscall.Kill(id, syscall.SIGKILL)
	}
}

func childPIDs(pid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var children []int
	for _, entry := range entries {
		id, err := strconv.Atoi(entry.Name())
		if err != nil || parentPID(id) != pid {
			continue
		}
		children = append(children, id)
	}
	return children
}

func parentPID(pid int) int {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	// comm is wrapped in parentheses and may contain spaces. ppid is the
	// second field after that closing parenthesis.
	text := string(data)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+2 >= len(text) {
		return 0
	}
	fields := strings.Fields(text[end+2:])
	if len(fields) < 2 {
		return 0
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return ppid
}
