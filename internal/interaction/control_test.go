package interaction

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDesktopLeaseCancellationAndHandoff(t *testing.T) {
	t.Parallel()
	c := New()
	release, err := c.Acquire(t.Context(), "one", "desktop")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = c.Acquire(ctx, "two", "desktop")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	c.Pause("one", "manual authentication")
	release()
	release()
	_, err = c.Acquire(t.Context(), "two", "desktop")
	require.ErrorContains(t, err, "interaction_paused")
	c.Resume("one")
	release, err = c.Acquire(t.Context(), "two", "desktop")
	require.NoError(t, err)
	release()
}

func TestParallelBrowserOwnersAndPersistedPause(t *testing.T) {
	t.Parallel()
	c := New()
	root := t.TempDir()
	a, err := c.Acquire(t.Context(), "parent", "browser/a")
	require.NoError(t, err)
	b, err := c.Acquire(t.Context(), "parent", "browser/b")
	require.NoError(t, err)
	a()
	require.Equal(t, "browser/b", c.Snapshot("parent").Active)
	b()
	c.Pause("parent", "user action")
	require.NoError(t, c.Record(root, "parent", Entry{Resource: "browser", Action: "handoff", Status: "paused"}))
	fresh := New()
	require.NoError(t, fresh.Load(root, "parent"))
	require.True(t, fresh.Snapshot("parent").Paused)
	require.Empty(t, fresh.Snapshot("parent").Active)
	_, err = fresh.Acquire(t.Context(), "parent", "browser/c")
	require.ErrorContains(t, err, "interaction_paused")
	state := fresh.Snapshot("parent")
	state.History[0].Action = "modified"
	require.Equal(t, "handoff", fresh.Snapshot("parent").History[0].Action)
}
