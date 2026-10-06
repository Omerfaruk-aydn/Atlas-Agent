//go:build !windows

package opencodecli

import "os/exec"

func hideWindow(_ *exec.Cmd) {}
