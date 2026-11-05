//go:build darwin

package procown

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// SZOMB is the Darwin process status for a zombie. A dead pid must not
// match an identity, even for the moment before it is reaped.
const darwinZombie = 5

// CreationTime returns the process start time in Unix milliseconds.
func CreationTime(pid int) (int64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("invalid pid %d", pid)
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, err
	}
	if int(info.Proc.P_pid) != pid || info.Proc.P_stat == darwinZombie {
		return 0, ErrNotOwned
	}
	started := info.Proc.P_starttime
	if started.Sec <= 0 {
		return 0, ErrNotOwned
	}
	return started.Sec*1000 + int64(started.Usec)/1000, nil
}
