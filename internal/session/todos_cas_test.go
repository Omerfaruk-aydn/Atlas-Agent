package session

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/stretchr/testify/require"
)

func TestFindingsTodoCASPreservesConcurrentUsage(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Release(dir)) })
	svc := NewService(db.New(conn), conn)
	sess, err := svc.Create(t.Context(), "CAS")
	require.NoError(t, err)
	before := TodosFingerprint(sess.Todos)
	require.NoError(t, svc.UpdateTitleAndUsage(t.Context(), sess.ID, "Concurrent title", 10, 20, 0.25))
	todos := []Todo{{ID: "repair", Content: "Repair finding", Status: TodoStatusPending}}
	saved, err := svc.CompareAndSwapTodos(t.Context(), sess.ID, before, todos)
	require.NoError(t, err)
	require.Equal(t, int64(10), saved.PromptTokens)
	require.Equal(t, 0.25, saved.Cost)
	require.Equal(t, "Concurrent title", saved.Title)
	_, err = svc.CompareAndSwapTodos(t.Context(), sess.ID, before, []Todo{{ID: "other", Content: "Overwrite", Status: TodoStatusPending}})
	require.Error(t, err)
	saved, err = svc.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "repair", saved.Todos[0].ID)
	// A legacy accounting save with an unchanged stale task snapshot must not
	// erase a newly registered remediation graph.
	sess.Cost = 0.35
	_, err = svc.Save(t.Context(), sess)
	require.NoError(t, err)
	saved, err = svc.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, saved.Todos, 1)
	require.Equal(t, "repair", saved.Todos[0].ID)
}
