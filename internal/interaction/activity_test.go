package interaction

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/stretchr/testify/require"
)

type activityFixtureRenderer struct{}

func (activityFixtureRenderer) Render(activity.Event) {}
func (activityFixtureRenderer) Close()                {}

func TestActivityStartInvalidatedByHandoff(t *testing.T) {
	t.Parallel()
	c := New()
	m := activity.New(activityFixtureRenderer{})
	defer m.Close()
	const id = "handoff-activity-fixture"
	finish := c.StartActivity(id, func() func() {
		c.Pause(id, "User control")
		return m.Begin(context.Background(), activity.Event{Session: id})
	})
	require.False(t, m.Snapshot().Visible)
	finish()
	require.True(t, m.Snapshot().FinishedUntil.IsZero())
	called := false
	c.StartActivity(id, func() func() { called = true; return func() {} })()
	require.False(t, called)
	c.Resume(id)
	finish = c.StartActivity(id, func() func() {
		return m.Begin(context.Background(), activity.Event{Session: id})
	})
	require.True(t, m.Snapshot().Visible)
	finish()
}
