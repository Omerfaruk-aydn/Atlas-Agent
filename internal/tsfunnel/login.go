package tsfunnel

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LoginTimeout bounds how long Atlas waits for the browser sign-in.
const LoginTimeout = 10 * time.Minute

// Login connects this computer to the user's own tailnet. It runs
// "tailscale up" without flags, which keeps the current profile and
// settings, and opens only the login.tailscale.com URL the CLI prints.
// Atlas never sees a password, auth key or token.
//
// "tailscale login" is not used: it switches to a new empty profile.
func Login(ctx context.Context, cli Exec, progress *Progress, open func(string) error) error {
	if progress == nil {
		progress = NewProgress()
	}
	ctx, cancel := context.WithTimeout(ctx, LoginTimeout)
	defer cancel()
	proc, err := cli.Start([]string{"up"}, progress.line)
	if err != nil {
		return err
	}
	opened := false
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			proc.Stop(3 * time.Second)
			return ctx.Err()
		case <-proc.Done():
			if code, _ := proc.Result(); code != 0 {
				detail := progress.lastLines()
				if strings.Contains(detail, "non-default flags") {
					return fmt.Errorf("this computer uses custom Tailscale settings; sign in from the Tailscale icon in the taskbar instead (%s)", detail)
				}
				return fmt.Errorf("tailscale up failed (code %d): %s", code, detail)
			}
			return nil
		case <-tick.C:
		}
		if u := progress.approvalURL(); u != "" && !opened {
			opened = true
			progress.Set(PhaseApproval)
			if open != nil {
				if err := open(u); err != nil {
					progress.fail(PhaseApproval, "open "+u+" in a browser")
				}
			}
		}
	}
}
