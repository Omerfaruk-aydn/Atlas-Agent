package activity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPointerFeedbackBelongsToActiveOperation(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(context.Background(), "pointer-fixture")
	defer end()
	m.Begin(ctx, Event{Session: "pointer-fixture"})
	old := m.Snapshot().ID
	m.Begin(ctx, Event{Session: "pointer-fixture"})
	id := m.Snapshot().ID
	require.False(t, m.Pointer(old, "click", 100, 200))
	require.True(t, m.Pointer(id, "move", -100, 200))
	e := m.Snapshot()
	require.Equal(t, "move", e.PointerKind)
	require.NotZero(t, e.PointerRevision)
	require.Equal(t, -100, e.X)
	require.True(t, m.Pointer(id, "click", 120, 220))
	require.Greater(t, m.Snapshot().PointerRevision, e.PointerRevision)
	require.True(t, m.PointerPoint(id, "aim", 120.25, 220.75))
	require.Equal(t, 120.25, m.Snapshot().PointerX)
	require.Equal(t, 220.75, m.Snapshot().PointerY)
	end()
	require.False(t, m.Pointer(id, "click", 100, 200))
}

func TestPointerCueReturnsToCalmIdle(t *testing.T) {
	now := time.Now()
	press, held := pointerCue(Event{PointerKind: "click", PointerAt: now}, now)
	require.Equal(t, float64(1), press)
	require.False(t, held)
	press, held = pointerCue(Event{PointerKind: "click", PointerAt: now}, now.Add(time.Second))
	require.Zero(t, press)
	require.False(t, held)
	press, held = pointerCue(Event{PointerKind: "drag_move"}, now)
	require.True(t, held)
	require.Equal(t, float64(1), press)
}

func TestOperationFinishDoesNotInventPointerRelease(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(t.Context(), "held-fixture")
	defer end()
	finish := m.Begin(ctx, Event{Session: "held-fixture"})
	require.True(t, m.Pointer(m.Snapshot().ID, "drag_move", 120, 220))
	finish()
	e := m.Snapshot()
	require.Equal(t, "wait", e.Action)
	require.Equal(t, "drag_move", e.PointerKind)
	require.True(t, e.Point)
	end()
	require.False(t, m.Snapshot().Visible)
}

func TestClickCueSurvivesNextToolOperation(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(t.Context(), "click-fixture")
	defer end()
	finish := m.Begin(ctx, Event{Session: "click-fixture"})
	require.True(t, m.Pointer(m.Snapshot().ID, "click", 120, 220))
	clicked := m.Snapshot()
	finish()
	m.Begin(ctx, Event{Session: "click-fixture", Action: "observe"})
	require.Equal(t, "click", m.Snapshot().PointerKind)
	require.Equal(t, clicked.PointerAt, m.Snapshot().PointerAt)
}

func TestPointerMotionSnapsForInputAndReducedMotion(t *testing.T) {
	var p pointerMotion
	start := time.Now()
	x, y := p.position(0, 0, false, start)
	require.Equal(t, float64(0), x)
	require.Equal(t, float64(0), y)
	p.position(100, 200, false, start)
	x, y = p.position(100, 200, false, start.Add(40*time.Millisecond))
	require.Greater(t, x, float64(0))
	require.Less(t, x, float64(100))
	require.Greater(t, y, float64(0))
	x, y = p.position(100, 200, true, start.Add(45*time.Millisecond))
	require.Equal(t, float64(100), x)
	require.Equal(t, float64(200), y)
	x, y = p.position(-80, 40, true, start.Add(time.Second))
	require.Equal(t, float64(-80), x)
	require.Equal(t, float64(40), y)
}
