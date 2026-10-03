package engineering

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirectivesSurviveRestartAndRespectTargetDependenciesAndBoundary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(dir)
	require.NoError(t, s.EnqueueDirective(t.Context(), "parent", UserDirective{TaskID: "backend", Mode: "next", Text: "Implement API", DependsOn: []string{"contract"}}))
	s = NewStore(dir)
	for _, input := range []struct {
		task  string
		first bool
		done  map[string]bool
	}{{"frontend", true, map[string]bool{"contract": true}}, {"backend", false, map[string]bool{"contract": true}}, {"backend", true, nil}} {
		directive, err := s.ClaimDirective(t.Context(), "parent", input.task, input.first, input.done)
		require.NoError(t, err)
		require.Nil(t, directive)
	}
	d, err := s.ClaimDirective(t.Context(), "parent", "backend", true, map[string]bool{"contract": true})
	require.NoError(t, err)
	require.NotNil(t, d)
	restarted := NewStore(dir)
	d2, err := restarted.ClaimDirective(t.Context(), "parent", "backend", true, map[string]bool{"contract": true})
	require.NoError(t, err)
	require.Nil(t, d2)
	_, board, err := restarted.ReadControlBoard(t.Context(), "parent")
	require.NoError(t, err)
	require.Equal(t, "claimed", board.Directives[0].Status)
	require.NoError(t, restarted.ReceiveDirective(t.Context(), "parent", d.ID))
	require.Error(t, restarted.ReceiveDirective(t.Context(), "parent", d.ID))
	_, board, err = restarted.ReadControlBoard(t.Context(), "parent")
	require.NoError(t, err)
	require.Equal(t, "received", board.Directives[0].Status)
	require.Positive(t, board.Directives[0].ReceivedAt)
	_, other, err := restarted.ReadControlBoard(t.Context(), "other")
	require.NoError(t, err)
	require.Empty(t, other.Directives)
}

func TestDirectiveConcurrentClaimAcrossStoreInstancesIsUnique(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(dir)
	require.NoError(t, s.EnqueueDirective(t.Context(), "session", UserDirective{Mode: "steer", Text: "Prioritize the failing test"}))
	var claimed atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			d, err := NewStore(dir).ClaimDirective(t.Context(), "session", "", false, nil)
			errs <- err
			if d != nil {
				claimed.Add(1)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, claimed.Load())
}

func TestInvalidBoardUpdateDoesNotPoisonPersistedState(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	require.Error(t, s.UpdateControlBoard(t.Context(), "s", func(b *ControlBoard) error { b.MaxAgents = 17; return nil }))
	require.Error(t, s.EnqueueDirective(t.Context(), "s", UserDirective{Mode: "steer", Text: "Review", File: "../secret", Line: 1}))
	require.Error(t, s.EnqueueDirective(t.Context(), "s", UserDirective{Mode: "next", Text: "Work", DependsOn: []string{"bad/task"}}))
	_, b, err := s.ReadControlBoard(t.Context(), "s")
	require.NoError(t, err)
	require.Empty(t, b.Directives)
	require.Zero(t, b.MaxAgents)
}

func TestRunnerTaskCancellationAndSharedLimit(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	a, cancelA := context.WithCancel(t.Context())
	defer cancelA()
	b, cancelB := context.WithCancel(t.Context())
	defer cancelB()
	unregisterA, err := s.RegisterRunner(t.Context(), "s", LiveRunner{SessionID: "a", TaskID: "api"}, cancelA)
	require.NoError(t, err)
	unregisterB, err := s.RegisterRunner(t.Context(), "s", LiveRunner{SessionID: "b", TaskID: "ui"}, cancelB)
	require.NoError(t, err)
	defer unregisterB()
	require.True(t, s.CancelTask("s", "api"))
	require.ErrorIs(t, a.Err(), context.Canceled)
	require.NoError(t, b.Err())
	require.False(t, s.CancelTask("s", "unknown"))
	require.NoError(t, s.UpdateControlBoard(t.Context(), "s", func(b *ControlBoard) error { b.MaxAgents = 1; return nil }))
	_, err = s.RegisterRunner(t.Context(), "s", LiveRunner{SessionID: "c", TaskID: "docs"}, func() {})
	require.ErrorContains(t, err, "limit")
	unregisterA()
	require.Len(t, s.LiveRunners("s"), 1)
	require.NoError(t, s.UpdateControlBoard(t.Context(), "s", func(b *ControlBoard) error { b.HeldTasks = []string{"docs"}; return nil }))
	_, err = s.RegisterRunner(t.Context(), "s", LiveRunner{SessionID: "c", TaskID: "docs"}, func() {})
	require.ErrorContains(t, err, "paused")
	_, err = s.Begin(t.Context(), "s", "call", "bash", "{}", "docs")
	require.ErrorContains(t, err, "paused")
}
