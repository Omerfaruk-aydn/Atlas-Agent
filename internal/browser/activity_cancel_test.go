package browser

import (
	"context"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/chromedp/chromedp"
	"github.com/stretchr/testify/require"
)

func TestActivityCancellationCleansSelectedAndPreviousTabs(t *testing.T) {
	if testing.Short() {
		t.Skip("Real Chrome excluded in short mode")
	}
	exe := findChrome()
	if exe == "" {
		t.Skip("Chrome not installed")
	}
	session, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir()})
	require.NoError(t, err)
	defer session.Close()
	s := session.(*chromedpSession)
	s.activityEnabled = true
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, finish := activity.StartFlow(parent, "cancel-tabs")
	defer finish()
	s.BeginActivity(ctx, "cancel-tabs", "snapshot", "")()
	count := func(tab context.Context) int {
		t.Helper()
		var result int
		require.NoError(t, chromedp.Run(tab, chromedp.Evaluate(`document.querySelectorAll('[data-atlas-activity]').length`, &result)))
		return result
	}
	old := s.currentContext()
	require.Eventually(t, func() bool { return count(old) == 1 }, time.Second, 10*time.Millisecond)
	_, _, err = s.Advanced(ctx, Request{Action: "tab_new"})
	require.NoError(t, err)
	require.Zero(t, count(old), "The background tab must not retain a running indicator")
	op := s.StartActivity(ctx, "cancel-tabs", "snapshot", "")
	s.getActivityManager().Present()
	require.Equal(t, 1, count(s.currentContext()))
	cancel()
	finish()
	require.Zero(t, count(s.currentContext()))
	require.Zero(t, count(old))
	op.End()
	s.StartActivity(ctx, "cancel-tabs", "snapshot", "").End()
	require.False(t, s.getActivityManager().Snapshot().Visible)
}
