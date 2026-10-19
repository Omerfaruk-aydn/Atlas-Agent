//go:build windows
// +build windows

package cmd

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detachProcess starts c outside the caller's console. It gets a hidden
// console of its own rather than none: a process with no console makes
// Windows open a visible window for every console program it launches
// (git, rg, shells), which flashes cmd windows on every run.
func detachProcess(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.HideWindow = true
	c.SysProcAttr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW
}
