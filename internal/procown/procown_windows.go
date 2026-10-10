//go:build windows

package procown

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type groupHandle = windows.Handle

func prepare(cmd *exec.Cmd, visible bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP, HideWindow: !visible}
}

// attach puts the child in a job object configured to kill every member
// when the job handle closes, so the whole tree can be stopped.
func attach(cmd *exec.Cmd) (groupHandle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func killGroup(cmd *exec.Cmd, job groupHandle) {
	if job != 0 {
		_ = windows.TerminateJobObject(job, 1)
		return
	}
	_ = cmd.Process.Kill()
}

func closeGroup(job groupHandle) {
	if job != 0 {
		_ = windows.CloseHandle(job)
	}
}

// CreationTime returns the process creation time in Unix milliseconds.
func CreationTime(pid int) (int64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err == nil && code != 259 { // STILL_ACTIVE
		return 0, ErrNotOwned
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	return created.Nanoseconds() / 1e6, nil
}

// killPID terminates a process and its descendants found by parent PID.
func killPID(pid int) error {
	for _, child := range children(pid) {
		_ = killPID(child)
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

func children(pid int) []int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var out []int
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if int(entry.ParentProcessID) == pid && int(entry.ProcessID) != pid {
			out = append(out, int(entry.ProcessID))
		}
	}
	return out
}
