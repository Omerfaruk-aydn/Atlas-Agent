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

func TestCancellationDuringHandoffSurvivesRestartAndStaysScoped(t *testing.T) {
	t.Parallel()
	c := New()
	root := t.TempDir()
	require.False(t, c.MarkCanceled("chat-a"), "an active conversation has no handoff to cancel")
	c.Pause("chat-a", "Approve the sign-in on your phone")
	c.Pause("chat-b", "Insert the security key")
	require.True(t, c.MarkCanceled("chat-a"))
	require.False(t, c.MarkCanceled("chat-a"), "repeated cancels do not bump the revision")
	require.NoError(t, c.Record(root, "chat-a", Entry{Resource: "control", Action: "cancel", Status: "canceled"}))

	restored := New()
	require.NoError(t, restored.Load(root, "chat-a"))
	state := restored.Snapshot("chat-a")
	require.True(t, state.Paused)
	require.True(t, state.Canceled, "cancellation must survive a restart")
	require.Equal(t, "Approve the sign-in on your phone", state.Reason)

	c.Resume("chat-a")
	require.False(t, c.Snapshot("chat-a").Paused)
	require.False(t, c.Snapshot("chat-a").Canceled)
	require.True(t, c.Snapshot("chat-b").Paused, "resuming one conversation must not resume another")
	require.False(t, c.Snapshot("chat-b").Canceled)

	c.Pause("chat-a", "Solve the image challenge")
	require.False(t, c.Snapshot("chat-a").Canceled, "a new handoff starts uncanceled")
}
