//go:build !windows

package cmd

import "os/exec"

// detachDesktopShell lets the app outlive this command and its terminal.
func detachDesktopShell(c *exec.Cmd) {
	detachProcess(c)
}
