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
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestResumeWorkflowReconcilesWithoutExecutingAndRejectsStalePlan(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package fixture\n"), 0o644))
	sess, err := env.sessions.Create(t.Context(), "resume")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "next", Content: "Implement the next slice", Status: session.TodoStatusPending, OwnedPaths: []string{"."}}}
	sess, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	env.permissions.SetMode(permission.ModeBypass)
	store := engineering.NewStore(t.TempDir())
	c := &coordinator{cfg: config.NewTestStore(&config.Config{}).Scoped(root), sessions: env.sessions, permissions: env.permissions, engineering: store}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	cp, err := c.captureCheckpoint(ctx, sess, "checkpoint")
	require.NoError(t, err)
	response, err := c.resumeWorkflow(ctx, WorkflowParams{Action: "resume-plan", CheckpointID: cp.ID}, fantasy.ToolCall{ID: "plan"})
	require.NoError(t, err)
	require.False(t, response.IsError)
	var plan engineering.ResumePlan
	require.NoError(t, json.Unmarshal([]byte(response.Content), &plan))
	require.Equal(t, []string{"next"}, plan.ReadyTasks)
	// A real interrupted operation makes the old schedule stale and the next
	// schedule ambiguous, regardless of whether its PID has been reused.
	op, err := store.Begin(ctx, sess.ID, "interrupted", "bash", `{}`, "")
	require.NoError(t, err)
	response, err = c.resumeWorkflow(ctx, WorkflowParams{Action: "resume", ResumePlan: &plan}, fantasy.ToolCall{ID: "stale"})
	require.NoError(t, err)
	require.True(t, response.IsError)
	response, err = c.resumeWorkflow(ctx, WorkflowParams{Action: "resume-plan", CheckpointID: cp.ID}, fantasy.ToolCall{ID: "ambiguous"})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(response.Content), &plan))
	require.Equal(t, []string{op}, plan.AmbiguousOperations)
	require.Empty(t, plan.ReadyTasks)
	require.NoError(t, store.Resolve(ctx, sess.ID, op, "abandoned", "Inspected actual command effects; no replay"))
	response, err = c.resumeWorkflow(ctx, WorkflowParams{Action: "resume-plan", CheckpointID: cp.ID}, fantasy.ToolCall{ID: "fresh"})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(response.Content), &plan))
	response, err = c.resumeWorkflow(ctx, WorkflowParams{Action: "resume", ResumePlan: &plan}, fantasy.ToolCall{ID: "apply"})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Contains(t, response.Content, `"commands_replayed":0`)
	state, err := store.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, state.Resume)
	require.Len(t, state.Operations, 1)
	require.Equal(t, "abandoned", state.Operations[0].Status)
}
