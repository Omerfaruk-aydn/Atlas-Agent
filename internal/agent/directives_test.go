package agent

import (
	"context"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestPersistedSteeringIsDeliveredToChildHistoryExactlyOnce(t *testing.T) {
	t.Parallel()
	a, env := newCancelTestAgent(t)
	a.engineering = engineering.NewStore(t.TempDir())
	parent, err := env.sessions.Create(t.Context(), "parent")
	require.NoError(t, err)
	child, err := env.sessions.CreateChild(t.Context(), parent.ID, "child")
	require.NoError(t, err)
	require.NoError(t, a.engineering.EnqueueDirective(t.Context(), parent.ID, engineering.UserDirective{TaskID: "api", Mode: "steer", Text: "Keep the existing public API", File: "internal/api.go", Line: 17}))
	ctx := engineering.WithScope(t.Context(), parent.ID, "api")
	messages, err := a.prepareDirectives(ctx, SessionAgentCall{SessionID: child.ID}, false)
	require.NoError(t, err)
	require.NotEmpty(t, messages)
	stored, err := env.messages.List(ctx, child.ID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Contains(t, stored[0].Content().Text, "Keep the existing public API")
	require.Contains(t, stored[0].Content().Text, "internal/api.go:17")
	_, board, err := a.engineering.ReadControlBoard(ctx, parent.ID)
	require.NoError(t, err)
	require.Equal(t, "received", board.Directives[0].Status)
	messages, err = a.prepareDirectives(ctx, SessionAgentCall{SessionID: child.ID}, false)
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestNextInstructionsStayOrderedAcrossModelTurns(t *testing.T) {
	t.Parallel()
	a, env := newCancelTestAgent(t)
	a.engineering = engineering.NewStore(t.TempDir())
	sess, err := env.sessions.Create(t.Context(), "queue")
	require.NoError(t, err)
	for _, text := range []string{"First task", "Second task"} {
		require.NoError(t, a.engineering.EnqueueDirective(t.Context(), sess.ID, engineering.UserDirective{Mode: "next", Text: text}))
	}
	call := SessionAgentCall{SessionID: sess.ID}
	messages, err := a.prepareDirectives(t.Context(), call, false)
	require.NoError(t, err)
	require.Empty(t, messages)
	messages, err = a.prepareDirectives(t.Context(), call, true)
	require.NoError(t, err)
	require.NotEmpty(t, messages)
	_, board, err := a.engineering.ReadControlBoard(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "received", board.Directives[0].Status)
	require.Equal(t, "queued", board.Directives[1].Status)
	ready, err := a.durableQueueReady(t.Context(), call)
	require.NoError(t, err)
	require.True(t, ready)
	require.NoError(t, a.engineering.Update(t.Context(), sess.ID, func(st *engineering.State) error { st.Paused = true; return nil }))
	ready, err = a.durableQueueReady(t.Context(), call)
	require.NoError(t, err)
	require.False(t, ready)
}

func TestHeldTasksRemainDependenciesWithoutReservingIdleOwnership(t *testing.T) {
	t.Parallel()
	a, env := newCancelTestAgent(t)
	a.engineering = engineering.NewStore(t.TempDir())
	sess, err := env.sessions.Create(t.Context(), "dependencies")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "base", Content: "Base", Status: session.TodoStatusCompleted}, {ID: "held", Content: "Held", Status: session.TodoStatusPending, DependsOn: []string{"base"}, OwnedPaths: []string{"api"}}, {ID: "free", Content: "Free", Status: session.TodoStatusPending, OwnedPaths: []string{"ui"}}, {ID: "dependent", Content: "Dependent", Status: session.TodoStatusPending, DependsOn: []string{"held"}, OwnedPaths: []string{"docs"}}}
	wave, err := session.ReadyTaskWaveExcluding(sess.Todos, 4, map[string]bool{"held": true})
	require.NoError(t, err)
	require.Len(t, wave, 1)
	require.Equal(t, "free", wave[0].ID)
}

func TestPersistentNextQueueRunsAfterCurrentStreamFinishes(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	model := &gatedStreamModel{text: "done", gate: make(chan struct{}), entered: make(chan struct{})}
	a := testSessionAgent(env, model, fastModel{}, "system").(*sessionAgent)
	a.engineering = engineering.NewStore(t.TempDir())
	sess, err := env.sessions.Create(t.Context(), "persistent queue")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "Current work"})
		done <- err
	}()
	select {
	case <-model.entered:
	case <-ctx.Done():
		t.Fatal("Model stream never started")
	}
	require.NoError(t, a.engineering.EnqueueDirective(ctx, sess.ID, engineering.UserDirective{Mode: "next", Text: "Second work"}))
	require.EqualValues(t, 1, model.calls.Load())
	close(model.gate)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("Persistent queue did not finish")
	}
	require.EqualValues(t, 2, model.calls.Load())
	_, board, err := a.engineering.ReadControlBoard(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, "received", board.Directives[0].Status)
}

func TestPausedContinuationNeverClaimsOrStartsModel(t *testing.T) {
	t.Parallel()
	a, env := newCancelTestAgent(t)
	a.engineering = engineering.NewStore(t.TempDir())
	sess, err := env.sessions.Create(t.Context(), "paused queue")
	require.NoError(t, err)
	require.NoError(t, a.engineering.EnqueueDirective(t.Context(), sess.ID, engineering.UserDirective{Mode: "next", Text: "Next work"}))
	require.NoError(t, a.engineering.Update(t.Context(), sess.ID, func(st *engineering.State) error { st.Paused = true; return nil }))
	_, err = a.prepareDirectives(t.Context(), SessionAgentCall{SessionID: sess.ID, QueueContinuation: true}, true)
	require.ErrorContains(t, err, "paused")
	_, board, err := a.engineering.ReadControlBoard(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "queued", board.Directives[0].Status)
}
