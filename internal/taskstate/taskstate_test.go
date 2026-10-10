package taskstate

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanceledIsTerminalAndLateEventsCannotRevive(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	r, err := s.Create(Record{ID: "run-1", Kind: "plan", State: Running})
	require.NoError(t, err)
	r, err = s.Move(r.ID, r.Revision, Canceled, "user stop")
	require.NoError(t, err)

	_, err = s.Move(r.ID, 0, Running, "late step event")
	require.ErrorIs(t, err, ErrTerminal)
	got, err := s.Get(r.ID)
	require.NoError(t, err)
	require.Equal(t, Canceled, got.State)

	// Re-applying the same terminal state is idempotent.
	_, err = s.Move(r.ID, 0, Canceled, "user stop")
	require.NoError(t, err)
}

func TestWaitingIsNotCancellation(t *testing.T) {
	t.Parallel()
	require.True(t, WaitingForPermission.Waiting())
	require.False(t, WaitingForPermission.Terminal())
	require.NoError(t, Transition(WaitingForPermission, Running))
	require.NoError(t, Transition(Reconnecting, Running))
}

func TestStaleRevisionIsRejected(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	r, err := s.Create(Record{ID: "env", Kind: "isolation", State: Preparing})
	require.NoError(t, err)
	_, err = s.Move(r.ID, r.Revision, Running, "")
	require.NoError(t, err)
	_, err = s.Move(r.ID, r.Revision, Paused, "stale writer")
	require.True(t, errors.Is(err, ErrRevision))
}

func TestInvalidIDsCannotEscapeTheStore(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	_, err := s.Create(Record{ID: "../escape"})
	require.Error(t, err)
	_, err = s.Get(`..\x`)
	require.Error(t, err)
}

func TestStartedRecordCannotReturnToQueued(t *testing.T) {
	t.Parallel()
	require.Error(t, Transition(Running, Queued))
	require.NoError(t, Transition(Queued, Queued))
}
