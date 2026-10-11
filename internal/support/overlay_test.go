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

