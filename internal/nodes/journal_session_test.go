package nodes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
