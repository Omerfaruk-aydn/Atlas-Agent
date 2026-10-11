package support

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/stretchr/testify/require"
)

// overlayRenderer keeps lifecycle tests independent of the real desktop.
type overlayRenderer struct{}

func (overlayRenderer) Render(activity.Event) {}
func (overlayRenderer) Close()                {}

func testActivityOverlay(t *testing.T) (*ActivityOverlay, *activity.Manager) {
	t.Helper()
	m := activity.New(overlayRenderer{})
	o := &ActivityOverlay{Manager: m, Language: "tr"}
	t.Cleanup(func() { o.End(); m.Close() })
	return o, m
}

func TestSupportConnectionDoesNotActivateUse(t *testing.T) {
	t.Parallel()
	o, m := testActivityOverlay(t)
	h := startHost(t, func(opts *HostOptions) { opts.Overlay = o })
	c := startController(t, nil)
	connect(t, h, c, Presets["diagnose"])
	require.Equal(t, StateConnected, h.Status().State)
	require.False(t, m.Snapshot().Visible)
	require.Zero(t, m.StopRevision())
	o.mu.Lock()
	defer o.mu.Unlock()
	require.Nil(t, o.work, "pairing must not start a control flow")
}

func TestIdleSupportDoesNotReplaceLocalActivity(t *testing.T) {
	t.Parallel()
	o, m := testActivityOverlay(t)
	op := m.Start(context.Background(), activity.Event{Session: "local", Resource: "computer", Action: "click"})
	defer op.End()
	before := m.Snapshot()
	o.Start("idle", "peer", func() { t.Error("Idle support was stopped") })
	require.Equal(t, before, m.Snapshot())
	o.End()
	require.Equal(t, before, m.Snapshot())
}

func TestSupportUseEndsAfterLastOperationWithoutDisconnecting(t *testing.T) {
	t.Parallel()
	o, m := testActivityOverlay(t)
	var stops atomic.Int32
	o.Start("one", "peer", func() { stops.Add(1) })
	first := o.Operation("click", 10, 20, true)
	second := o.Operation("type", 0, 0, false)
	require.True(t, m.Snapshot().Visible)
	require.True(t, m.Snapshot().CanStop)
	require.Equal(t, "tr", m.Snapshot().Language)
	first()
	first()
	require.True(t, m.Snapshot().Visible, "overlapping work remains visible")
	second()
	require.False(t, m.Snapshot().Visible)
	require.Zero(t, m.StopRevision())
	require.Zero(t, stops.Load())
	third := o.Operation("click", 0, 0, false)
	require.True(t, m.Snapshot().Visible, "the connection remains usable")
	second()
	require.True(t, m.Snapshot().Visible, "an old completion cannot hide new work")
	third()
	require.False(t, m.Snapshot().Visible)
	require.Zero(t, stops.Load())
}

func TestSupportBannerStopStillRevokesSession(t *testing.T) {
	t.Parallel()
	o, m := testActivityOverlay(t)
	h := startHost(t, func(opts *HostOptions) { opts.Overlay = o })
	c := startController(t, nil)
	connect(t, h, c, Presets["diagnose"])
	end := o.Operation("wait", 0, 0, false)
	require.True(t, m.Stop(m.Snapshot().ID))
	require.Eventually(t, func() bool { return h.Status().State == StateRevoked }, time.Second, time.Millisecond)
	require.False(t, m.Snapshot().Visible)
	end()
	require.False(t, m.Snapshot().Visible)
}

func TestSupportOldCompletionCannotAffectNewSession(t *testing.T) {
	t.Parallel()
	o, m := testActivityOverlay(t)
	var stops atomic.Int32
	o.Start("old", "peer", func() { stops.Add(1) })
	old := o.Operation("type", 0, 0, false)
	o.Start("new", "peer", func() { stops.Add(1) })
	require.False(t, m.Snapshot().Visible)
	current := o.Operation("click", 0, 0, false)
	old()
	require.True(t, m.Snapshot().Visible)
	require.Equal(t, "support-new", m.Snapshot().Session)
	o.End()
	require.False(t, m.Snapshot().Visible)
	current()
	require.Zero(t, stops.Load())
	o.Operation("click", 0, 0, false)()
	require.False(t, m.Snapshot().Visible)
}
