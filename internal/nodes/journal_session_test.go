package nodes

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/stretchr/testify/require"
)

func TestJournalBeginReturnsIndependentSnapshot(t *testing.T) {
	t.Parallel()
	j, err := OpenJournal(t.TempDir())
	require.NoError(t, err)
	op := Operation{ID: "op-begin-snapshot", DeviceID: "device", RequestHash: "request"}
	started, replayed, err := j.Begin(op)
	require.NoError(t, err)
	require.False(t, replayed)
	_, err = j.Finish(op.ID, func(record *Operation) {
		record.State = taskstate.Canceled
	})
	require.NoError(t, err)
	require.Equal(t, taskstate.Running, started.State)
	stored, ok := j.Get(op.ID, op.DeviceID)
	require.True(t, ok)
	require.Equal(t, taskstate.Canceled, stored.State)
}

func TestJournalCannotReplayOrExposeAnotherSupportSession(t *testing.T) {
	t.Parallel()
	j, err := OpenJournal(t.TempDir())
	require.NoError(t, err)
	op := Operation{ID: "op-session-isolation", DeviceID: "same-device", SessionID: "first-session", RequestHash: "same-request"}
	_, _, err = j.Begin(op)
	require.NoError(t, err)
	_, exists := j.Get(op.ID, op.DeviceID, "new-session")
	require.False(t, exists)
	_, err = j.Replay(op.ID, op.DeviceID, op.RequestHash, "new-session")
	require.ErrorIs(t, err, ErrOperationConflict)
	op.SessionID = "new-session"
	_, _, err = j.Begin(op)
	require.ErrorIs(t, err, ErrOperationConflict)
	// A reconnect in the same session still returns the original record.
	replay, err := j.Replay(op.ID, op.DeviceID, op.RequestHash, "first-session")
	require.NoError(t, err)
	require.True(t, replay.Replayed)
}
