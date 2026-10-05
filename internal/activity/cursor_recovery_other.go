//go:build !windows

package activity

// RunCursorRecovery is only used by the Windows cursor watchdog process.
func RunCursorRecovery() bool { return false }
