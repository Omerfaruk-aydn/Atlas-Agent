package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/stretchr/testify/require"
)

func TestGateRefusalIsMarkedAsRejectedNotStorageFailure(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Release(dir)) })
	gate := func(context.Context, Session, Session) error {
		return errors.New("task esp-boxes is outside the current delivery stage or changed; revise the plan before execution")
	}
	svc := NewService(db.New(conn), conn, gate)
	sess, err := svc.Create(t.Context(), "rejected")
	require.NoError(t, err)
	sess.Todos = []Todo{{ID: "esp-boxes", Content: "Build boxes", Status: TodoStatusInProgress}}
	_, err = svc.Save(t.Context(), sess)
	require.ErrorIs(t, err, ErrTodosRejected)
	require.ErrorContains(t, err, "revise the plan before execution")
}
