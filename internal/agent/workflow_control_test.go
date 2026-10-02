package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestWorkflowSnapshotAndControlRevisionReassignment(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	sess, err := env.sessions.Create(t.Context(), "workflow")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "task", Content: "Implement feature", Status: session.TodoStatusPending, Agent: "backend", AcceptanceCriteria: []string{"Works"}}}
	sess, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	c := &coordinator{sessions: env.sessions, engineering: store, cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(root)}
	first, err := c.WorkflowSnapshot(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, first.Tasks, 1)
	require.NoError(t, c.WorkflowControl(t.Context(), sess.ID, engineering.WorkflowControl{Action: "reassign", TaskID: "task", Agent: "frontend", ExpectedRevision: first.Revision}))
	second, err := c.WorkflowSnapshot(t.Context(), sess.ID)
	require.NoError(t, err)
	require.NotEqual(t, first.Revision, second.Revision)
	require.NotEqual(t, first.Tasks[0].SpecFingerprint, second.Tasks[0].SpecFingerprint)
	require.ErrorContains(t, c.WorkflowControl(t.Context(), sess.ID, engineering.WorkflowControl{Action: "pause", ExpectedRevision: first.Revision}), "revision conflict")
	require.NoError(t, c.WorkflowControl(t.Context(), sess.ID, engineering.WorkflowControl{Action: "pause", ExpectedRevision: second.Revision}))
	_, err = store.Begin(t.Context(), sess.ID, "blocked", "agent", "{}", "task")
	require.ErrorContains(t, err, "paused")
	paused, err := c.WorkflowSnapshot(t.Context(), sess.ID)
	require.NoError(t, err)
	require.True(t, paused.Paused)
	require.NoError(t, c.WorkflowControl(t.Context(), sess.ID, engineering.WorkflowControl{Action: "resume", ExpectedRevision: paused.Revision}))
	op, err := store.Begin(t.Context(), sess.ID, "active", "bash", "{}", "task")
	require.NoError(t, err)
	active, err := c.WorkflowSnapshot(t.Context(), sess.ID)
	require.NoError(t, err)
	require.ErrorContains(t, c.WorkflowControl(t.Context(), sess.ID, engineering.WorkflowControl{Action: "reassign", TaskID: "task", Agent: "backend", ExpectedRevision: active.Revision}), "reconcile")
	require.NoError(t, store.Finish(t.Context(), sess.ID, op, true, false))
	before, err := c.WorkflowSnapshot(t.Context(), sess.ID)
	require.NoError(t, err)
	_, err = store.PutRecord(t.Context(), "scenario-runs-"+engineering.Hash(filepath.Clean(root)+"\x00"+sess.ID)+"-aa", "record", 0, []byte(`{}`))
	require.NoError(t, err)
	after, err := c.WorkflowSnapshot(t.Context(), sess.ID)
	require.NoError(t, err)
	require.NotEqual(t, before.Revision, after.Revision)
	t.Setenv("ATLAS_TEST_SECRET", "never-serialize-provider-token")
	encoded, err := json.Marshal(after)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), os.Getenv("ATLAS_TEST_SECRET"))
}

func TestWorkflowPauseBeforeAdmissionRestoresPendingTask(t *testing.T) {
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "pause-race")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "task", Content: "Implement", Status: session.TodoStatusPending, Agent: "backend", OwnedPaths: []string{"."}, AcceptanceCriteria: []string{"Works"}}}
	sess, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	store := engineering.NewStore(t.TempDir())
	c := &coordinator{sessions: env.sessions, engineering: store, cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(t.TempDir())}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	target := fantasy.NewAgentTool("agent", "Must never start", func(context.Context, AgentParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		t.Fatal("Paused agent started")
		return fantasy.ToolResponse{}, nil
	})
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		require.NoError(t, store.Update(ctx, sess.ID, func(st *engineering.State) error { st.Paused = true; return nil }))
		return (&guardedTool{AgentTool: target, store: store}).Run(ctx, call)
	}
	_, err = c.dispatchWave(ctx, WorkflowParams{}, fantasy.ToolCall{ID: "wave"}, sess, invoke)
	require.NoError(t, err)
	latest, err := env.sessions.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, session.TodoStatusPending, latest.Todos[0].Status)
	st, err := store.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.Empty(t, st.Operations)
}
