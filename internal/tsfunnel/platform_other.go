//go:build !windows

package tsfunnel

import "os/exec"

func hideWindow(*exec.Cmd) {}

// SystemServiceState is unknown outside Windows; the CLI reports a
// missing daemon itself.
func SystemServiceState() ServiceState { return ServiceUnknown }
