package activity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFlowKeepsCursorBetweenToolsUntilRunEnds(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(context.Background(), "flow-fixture")
	finish := m.Begin(ctx, Event{Session: "flow-fixture", Point: true, X: 40, Y: 50})
	finish()
	require.True(t, m.Snapshot().Visible)
	require.True(t, m.Snapshot().Persistent)
	require.False(t, m.Snapshot().Point)
	require.True(t, m.Snapshot().FinishedUntil.IsZero())
	end()
	require.False(t, m.Snapshot().Visible)
	finish()
	require.False(t, m.Snapshot().Visible)
}

func TestFlowCancellationAndOldFlowDoNotAffectNewRun(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	parent, cancel := context.WithCancel(context.Background())
	ctx, end := StartFlow(parent, "flow-cancel")
	defer end()
	m.Begin(ctx, Event{Session: "flow-cancel"})()
	cancel()
	require.Eventually(t, func() bool { return !m.Snapshot().Visible }, time.Second, time.Millisecond)
	old, endOld := StartFlow(context.Background(), "same-session")
	m.Begin(old, Event{Session: "same-session"})()
	next, endNext := StartFlow(context.Background(), "same-session")
	defer endNext()
	m.Begin(next, Event{Session: "same-session"})()
	endOld()
	require.True(t, m.Snapshot().Visible)
	endNext()
	require.False(t, m.Snapshot().Visible)
}

func TestFlowSwitchesOneVisibleSurfaceBetweenDesktopAndBrowser(t *testing.T) {
	desktop := New(&testRenderer{})
	defer desktop.Close()
	browser := New(&testRenderer{})
	defer browser.Close()
	ctx, end := StartFlow(context.Background(), "switch-fixture")
	defer end()
	desktop.Begin(ctx, Event{Session: "switch-fixture", Resource: "desktop"})()
	browser.Begin(ctx, Event{Session: "switch-fixture", Resource: "browser"})()
	require.False(t, desktop.Snapshot().Visible)
	require.True(t, browser.Snapshot().Visible)
	desktop.Begin(ctx, Event{Session: "switch-fixture", Resource: "desktop"})()
	require.False(t, browser.Snapshot().Visible)
	require.True(t, desktop.Snapshot().Visible)
}

func TestRunOwnedCursorStaysVisibleDuringObservation(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(context.Background(), "observe-fixture")
	defer end()
	m.Begin(ctx, Event{Session: "observe-fixture"})()
	restore := m.SuspendForObservation()
	require.True(t, m.Snapshot().Visible)
	restore()
	require.True(t, m.Snapshot().Visible)
}

func TestBannerStopCancelsOnlyItsOwnedRun(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(context.Background(), "stop-fixture")
	defer end()
	m.Begin(ctx, Event{Session: "stop-fixture"})()
	oldID := m.Snapshot().ID
	m.Begin(ctx, Event{Session: "stop-fixture"})()
	stopper, ok := any(m).(interface{ Stop(uint64) bool })
	require.True(t, ok, "The control banner needs a run-scoped stop handler")
	require.False(t, stopper.Stop(oldID), "A stale banner cannot cancel the current operation")
	require.Equal(t, m.Snapshot().ID, m.StopRevision())
	resume := m.Suspend()
	require.Zero(t, m.StopRevision(), "A suspended banner cannot advertise a stop owner")
	resume()
	require.Equal(t, m.Snapshot().ID, m.StopRevision())
	require.NoError(t, ctx.Err())
	require.True(t, stopper.Stop(m.Snapshot().ID))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Eventually(t, func() bool { return !m.Snapshot().Visible }, time.Second, time.Millisecond)
	require.Zero(t, m.StopRevision())
}

func TestPausedFlowDoesNotRestoreStaleActivity(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(t.Context(), "paused-flow")
	defer end()
	m.Begin(ctx, Event{Session: "paused-flow"})()
	oldID := m.Snapshot().ID
	resume := PauseFlow(ctx, "paused-flow")
	resumeNested := PauseFlow(ctx, "paused-flow")
	require.False(t, m.Snapshot().Visible)
	require.False(t, m.Stop(oldID))
	require.False(t, m.Pointer(oldID, "click", 20, 30))
	resume()
	resume()
	m.Begin(ctx, Event{Session: "paused-flow"})()
	require.False(t, m.Snapshot().Visible, "A second unanswered question keeps the flow paused")
	resumeNested()
	require.False(t, m.Snapshot().Visible)
	m.Begin(ctx, Event{Session: "paused-flow"})()
	require.True(t, m.Snapshot().Visible)
	resumeLate := PauseFlow(ctx, "paused-flow")
	end()
	resumeLate()
	m.Begin(ctx, Event{Session: "paused-flow"})()
	require.False(t, m.Snapshot().Visible, "A cancelled flow cannot regain control")
}

func TestPauseFlowPreservesOtherRunOwnership(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	old, endOld := StartFlow(t.Context(), "same-owner")
	defer endOld()
	m.Begin(old, Event{Session: "same-owner"})()
	current, endCurrent := StartFlow(t.Context(), "same-owner")
	defer endCurrent()
	m.Begin(current, Event{Session: "same-owner"})()
	currentID := m.Snapshot().ID
	resume := PauseFlow(old, "same-owner")
	defer resume()
	require.True(t, m.Snapshot().Visible)
	require.Equal(t, currentID, m.Snapshot().ID)
}

func TestCancelSessionFlushesSurfacesAndRejectsLateActivity(t *testing.T) {
	r := &testRenderer{}
	m := New(r)
	defer m.Close()
	ctx, finish := StartRun(t.Context(), "cancel-surface")
	defer finish(false)
	op := m.Start(ctx, Event{Session: "cancel-surface", Resource: "browser", Point: true})
	m.Present()
	oldID := m.Snapshot().ID
	other := New(&testRenderer{})
	defer other.Close()
	otherCtx, endOther := StartFlow(t.Context(), "other-surface")
	defer endOther()
	other.Begin(otherCtx, Event{Session: "other-surface"})()
	CancelSession("cancel-surface")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, r.last().Visible, "Cancellation must flush the renderer before returning")
	require.False(t, r.last().CanStop)
	require.False(t, r.last().Persistent)
	require.Zero(t, m.StopRevision())
	require.False(t, m.Pointer(oldID, "click", 10, 20))
	op.End()
	m.Start(ctx, Event{Session: "cancel-surface"}).End()
	require.False(t, m.Snapshot().Visible)
	require.True(t, other.Snapshot().Visible)
}

func TestBannerStopFlushesRendererBeforeReturning(t *testing.T) {
	r := &testRenderer{}
	m := New(r)
	defer m.Close()
	ctx, finish := StartFlow(t.Context(), "stop-flush")
	defer finish()
	m.Begin(ctx, Event{Session: "stop-flush"})()
	m.Present()
	require.True(t, m.Stop(m.Snapshot().ID))
	require.False(t, r.last().Visible)
	require.True(t, r.last().FinishedUntil.IsZero())
}
