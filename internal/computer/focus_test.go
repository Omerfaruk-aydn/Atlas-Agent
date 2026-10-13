package computer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeFocus struct {
	exists, visible bool
	identity        WindowIdentity
	foreground      bool
	// succeedAt is the attempt index that makes the window foreground.
	succeedAt int
	calls     []string
	// recreateAfter replaces the window identity during this attempt.
	recreateAfter int
}

func (f *fakeFocus) ops() focusOps {
	attempt := func(i int, m string) focusAttempt {
		return focusAttempt{Method: m, Run: func() error {
			f.calls = append(f.calls, m)
			if i == f.recreateAfter {
				f.identity = WindowIdentity{ProcessID: 999, ClassName: "Other"}
			}
			if i == f.succeedAt {
				f.foreground = true
				return nil
			}
			return errors.New("refused")
		}}
	}
	return focusOps{
		Exists:     func() bool { return f.exists },
		Visible:    func() bool { return f.visible },
		Identity:   func() WindowIdentity { return f.identity },
		Restore:    func() {},
		Foreground: func() bool { return f.foreground },
		Attempts:   []focusAttempt{attempt(0, "set_foreground"), attempt(1, "attach_thread_input"), attempt(2, "alt_unlock")},
		Settle:     func(context.Context, time.Duration) bool { return f.foreground },
	}
}

var chromeWindow = WindowIdentity{ProcessID: 14124, ClassName: "Chrome_WidgetWin_1"}

// Reported session: SetForegroundWindow was refused (foreground lock) and
// focus failed at once. Recovery now escalates and says which method won.
func TestFocusEscalatesPastTheForegroundLock(t *testing.T) {
	t.Parallel()
	f := &fakeFocus{exists: true, visible: true, identity: chromeWindow, succeedAt: 1, recreateAfter: -1}
	res, err := focusWithRecovery(t.Context(), "198000", chromeWindow, f.ops(), time.Millisecond)
	require.NoError(t, err)
	require.True(t, res.Focused)
	require.Equal(t, "attach_thread_input", res.Method)
	require.Equal(t, []string{"set_foreground", "attach_thread_input"}, f.calls)
}

func TestFocusFailsExplicitlyWhenEveryMethodIsRefused(t *testing.T) {
	t.Parallel()
	f := &fakeFocus{exists: true, visible: true, identity: chromeWindow, succeedAt: -1, recreateAfter: -1}
	_, err := focusWithRecovery(t.Context(), "198000", chromeWindow, f.ops(), time.Millisecond)
	require.ErrorContains(t, err, "focus_denied")
	require.Len(t, f.calls, 3, "recovery is bounded to the listed methods")
}

// A handle reused by a recreated window is never activated or reported.
func TestFocusRefusesAReusedWindowHandle(t *testing.T) {
	t.Parallel()
	f := &fakeFocus{exists: true, visible: true, identity: WindowIdentity{ProcessID: 31116, ClassName: "Chrome_WidgetWin_1"}, succeedAt: 0, recreateAfter: -1}
	_, err := focusWithRecovery(t.Context(), "198000", chromeWindow, f.ops(), time.Millisecond)
	require.ErrorContains(t, err, "stale_target")
	require.Empty(t, f.calls)

	// Recreated during recovery: the success is not reported.
	g := &fakeFocus{exists: true, visible: true, identity: chromeWindow, succeedAt: 1, recreateAfter: 1}
	_, err = focusWithRecovery(t.Context(), "198000", chromeWindow, g.ops(), time.Millisecond)
	require.ErrorContains(t, err, "stale_target")
}

func TestFocusIsCancellable(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f := &fakeFocus{exists: true, visible: true, identity: chromeWindow, succeedAt: 2, recreateAfter: -1}
	_, err := focusWithRecovery(ctx, "1", chromeWindow, f.ops(), time.Millisecond)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, f.calls)
}

func TestFocusMissingOrHiddenWindow(t *testing.T) {
	t.Parallel()
	_, err := focusWithRecovery(t.Context(), "1", chromeWindow, (&fakeFocus{identity: chromeWindow, recreateAfter: -1}).ops(), time.Millisecond)
	require.ErrorContains(t, err, "target_missing")
	_, err = focusWithRecovery(t.Context(), "1", chromeWindow, (&fakeFocus{exists: true, identity: chromeWindow, recreateAfter: -1}).ops(), time.Millisecond)
	require.ErrorContains(t, err, "target_not_actionable")
}
