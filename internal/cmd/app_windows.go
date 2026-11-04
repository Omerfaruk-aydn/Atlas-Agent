//go:build windows

package cmd

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detachDesktopShell lets the app outlive this command and its console. Unlike
// detachProcess it must not set HideWindow: that passes SW_HIDE to the child,
// and the app's first window would open invisible.
func detachDesktopShell(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS
}
