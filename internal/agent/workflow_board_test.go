package agent

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestUserControlsCancelOnlySelectedRunnerAndAllowIndependentReassignment(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "controls")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "api", Content: "API", Agent: "backend", Status: session.TodoStatusPending, OwnedPaths: []string{"api"}}, {ID: "ui", Content: "UI", Agent: "frontend", Status: session.TodoStatusPending, OwnedPaths: []string{"ui"}}}
	sess, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	s := engineering.NewStore(t.TempDir())
	c := &coordinator{sessions: env.sessions, engineering: s, cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(t.TempDir())}
	api, cancelAPI := context.WithCancel(t.Context())
	defer cancelAPI()
	ui, cancelUI := context.WithCancel(t.Context())
	defer cancelUI()
	unregister, err := s.RegisterRunner(t.Context(), sess.ID, engineering.LiveRunner{TaskID: "api", SessionID: "api-child"}, cancelAPI)
	require.NoError(t, err)
	apply := func(action engineering.WorkflowControl) error {
		snapshot, err := c.WorkflowSnapshot(t.Context(), sess.ID)
		if err != nil {
			return err
		}
		action.ExpectedRevision = snapshot.Revision
		return c.WorkflowControl(t.Context(), sess.ID, action)
	}
	require.NoError(t, apply(engineering.WorkflowControl{Action: "reassign", TaskID: "ui", Agent: "backend"}))
	unregisterUI, err := s.RegisterRunner(t.Context(), sess.ID, engineering.LiveRunner{TaskID: "ui", SessionID: "ui-child"}, cancelUI)
	require.NoError(t, err)
	defer unregisterUI()
	require.NoError(t, apply(engineering.WorkflowControl{Action: "cancel_task", TaskID: "api"}))
	require.ErrorIs(t, api.Err(), context.Canceled)
	require.NoError(t, ui.Err())
	require.ErrorContains(t, apply(engineering.WorkflowControl{Action: "task_retry", TaskID: "api"}), "reconcile")
	unregister()
	require.NoError(t, apply(engineering.WorkflowControl{Action: "task_resume", TaskID: "api"}))
	require.NoError(t, s.Check(t.Context(), sess.ID, "api"))
	require.ErrorContains(t, apply(engineering.WorkflowControl{Action: "queue", Text: "Work", DependsOn: []string{"missing"}}), "dependency")
	require.Error(t, apply(engineering.WorkflowControl{Action: "feedback", Text: "Fix", File: "../outside", Line: 1}))
	require.NoError(t, apply(engineering.WorkflowControl{Action: "queue", Text: "Next API work", TaskID: "api", DependsOn: []string{"ui"}}))
	_, board, err := s.ReadControlBoard(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, board.Directives, 1)
	require.NoError(t, apply(engineering.WorkflowControl{Action: "queue_cancel", DirectiveID: board.Directives[0].ID}))
	require.NoError(t, apply(engineering.WorkflowControl{Action: "queue_remove", DirectiveID: board.Directives[0].ID}))
	_, board, err = s.ReadControlBoard(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Empty(t, board.Directives)
}

func TestTaskRevisionInvalidatesQualityAndRejectsEscapingScope(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "revision")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "api", Content: "Original", Agent: "backend", Status: session.TodoStatusPending, OwnedPaths: []string{"api"}}}
	sess, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	s := engineering.NewStore(t.TempDir())
	c := &coordinator{sessions: env.sessions, engineering: s, cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(t.TempDir())}
	require.NoError(t, s.Update(t.Context(), sess.ID, func(st *engineering.State) error {
		st.RoleExecutions["api"] = engineering.RoleExecution{TaskID: "api", Passed: true}
		return nil
	}))
	snapshot, err := c.WorkflowSnapshot(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Error(t, c.WorkflowControl(t.Context(), sess.ID, engineering.WorkflowControl{Action: "task_scope", TaskID: "api", OwnedPaths: []string{"../outside"}, ExpectedRevision: snapshot.Revision}))
	require.NoError(t, c.WorkflowControl(t.Context(), sess.ID, engineering.WorkflowControl{Action: "task_replan", TaskID: "api", Text: "Revised API requirement", ExpectedRevision: snapshot.Revision}))
	state, err := s.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Empty(t, state.RoleExecutions)
	sess, err = env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "Revised API requirement", sess.Todos[0].Content)
	require.Equal(t, "pending", sess.Todos[0].Verification)
}
