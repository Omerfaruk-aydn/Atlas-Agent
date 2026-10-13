//go:build windows

package tsfunnel

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// windowsService is the service name the official installer registers.
const windowsService = "Tailscale"

// SystemServiceState queries the Tailscale Windows service with
// connect and query rights only, which every user has. It never starts,
// stops or reconfigures the service.
func SystemServiceState() ServiceState {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return ServiceUnknown
	}
	defer windows.CloseServiceHandle(scm)
	name, err := windows.UTF16PtrFromString(windowsService)
	if err != nil {
		return ServiceUnknown
	}
	svc, err := windows.OpenService(scm, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return ServiceMissing
		}
		return ServiceUnknown
	}
	defer windows.CloseServiceHandle(svc)
	var status windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(svc, &status); err != nil {
		return ServiceUnknown
	}
	switch status.CurrentState {
	case windows.SERVICE_RUNNING:
		return ServiceRunning
	case windows.SERVICE_START_PENDING, windows.SERVICE_CONTINUE_PENDING:
		return ServiceStarting
	default:
		return ServiceStopped
	}
}
