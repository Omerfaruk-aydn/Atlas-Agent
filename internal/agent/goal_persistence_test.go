package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestGoalProgressSurvivesCoordinatorReplacement(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Release(dir)) })
	service := session.NewService(db.New(conn), conn)
	sess, err := service.Create(t.Context(), "goal")
	require.NoError(t, err)
	c := goalTestCoordinator(sess.ID, nil)
	c.sessions = service
	require.NoError(t, c.StartGoal(t.Context(), sess.ID, "finish"))
	run, ok := c.goalRunFor(sess.ID)
	require.True(t, ok)
	run.mu.Lock()
	run.budget = 30
	run.used = 7
	run.mu.Unlock()
	require.NoError(t, c.persistGoal(t.Context(), sess.ID, run, false))
	resumed := goalTestCoordinator(sess.ID, nil)
	resumed.sessions = service
	next, ok := resumed.resumeGoalRun(t.Context(), sess.ID)
	require.True(t, ok)
	require.Equal(t, 7, next.used)
	require.Equal(t, 30, next.budget)
	require.NoError(t, resumed.persistGoal(t.Context(), sess.ID, next, true))
	restarted := goalTestCoordinator(sess.ID, nil)
	restarted.sessions = service
	restored, ok := restarted.resumeGoalRun(t.Context(), sess.ID)
	require.True(t, ok)
	require.Equal(t, 8, restored.used)
	require.Error(t, resumed.persistGoal(t.Context(), sess.ID, next, true))
	require.NoError(t, restarted.ClearGoal(t.Context(), sess.ID))
	_, ok = restarted.resumeGoalRun(t.Context(), sess.ID)
	require.False(t, ok)
	require.NoError(t, c.StartGoal(t.Context(), sess.ID, "finish"))
	require.Error(t, c.persistGoal(t.Context(), sess.ID, run, false))
}
