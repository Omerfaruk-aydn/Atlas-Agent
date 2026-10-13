
import (
	"context"
	"fmt"
	"time"
)

// WindowIdentity pins a window handle to the process and class that owned
// it when it was observed. Windows reuses handle values, so a handle alone
// may name a different, newly created window later.
type WindowIdentity struct {
	ProcessID uint32
	ClassName string
}

// focusOps are the platform calls the focus recovery uses. They are a
// struct of functions so the escalation order and its identity checks are
// testable without a desktop.
type focusOps struct {
	Exists     func() bool
	Visible    func() bool
	Identity   func() WindowIdentity
	Restore    func()
	Foreground func() bool
	// Attempts are tried in order until the window is foreground. Each
	// is bounded; none sends input to the target window.
	Attempts []focusAttempt
	// Settle waits for asynchronous activation; it returns early when
	// the window becomes foreground and fails when ctx is done.
	Settle func(ctx context.Context, d time.Duration) bool
}

type focusAttempt struct {
	Method string
	Run    func() error
}

// FocusResult reports how a window was activated.
type FocusResult struct {
	Focused  bool     `json:"focused"`
	WindowID string   `json:"window_id"`
	Method   string   `json:"method"`
	Tried    []string `json:"tried"`
}

// focusWithRecovery activates the window identified by id, escalating
// through ops.Attempts. Before every attempt, and before reporting success,
// it re-checks that the handle still names the same process and class, so
// a handle reused by a recreated window is never activated or reported as
// focused. It never sends input to the target.
func focusWithRecovery(ctx context.Context, id string, want WindowIdentity, ops focusOps, settle time.Duration) (FocusResult, error) {
	res := FocusResult{WindowID: id}
	check := func() error {
		if !ops.Exists() {
			return fmt.Errorf("target_missing: window %s disappeared; list windows again", id)
		}
		if got := ops.Identity(); got != want {
			return fmt.Errorf("stale_target: window %s now belongs to %s (pid %d), not %s (pid %d); list windows again", id, got.ClassName, got.ProcessID, want.ClassName, want.ProcessID)
		}
		if !ops.Visible() {
			return fmt.Errorf("target_not_actionable: window %s is hidden", id)
		}
		return nil
	}
	if err := check(); err != nil {
		return res, err
	}
	ops.Restore()
	for _, attempt := range ops.Attempts {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if err := check(); err != nil {
			return res, err
		}
		res.Tried = append(res.Tried, attempt.Method)
		if err := attempt.Run(); err != nil {
			continue
		}
		if ops.Foreground() || ops.Settle(ctx, settle) {
			if err := check(); err != nil {
				return res, err
			}
			res.Focused, res.Method = true, attempt.Method
			return res, nil
		}
		if err := ctx.Err(); err != nil {
			return res, err
		}
	}
	return res, fmt.Errorf("focus_denied: Windows did not activate window %s after %v; ask the person at the computer to click the window, then observe again before input", id, res.Tried)
}
