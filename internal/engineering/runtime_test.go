package engineering

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJournalSurvivesRestartAndNeverReplays(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	id, err := s.Begin(t.Context(), "session", "tool-1", "bash", `{"command":"secret"}`, "task")
	require.NoError(t, err)
	restarted := NewStore(dir)
	state, err := restarted.Read(t.Context(), "session")
	require.NoError(t, err)
	require.Len(t, state.Operations, 1)
	require.Equal(t, "running", state.Operations[0].Status)
	require.NotContains(t, state.Operations[0].Fingerprint, "secret")
	require.NoError(t, restarted.Finish(t.Context(), "session", id, true, false))
}

func TestNestedScopePreservesOwnershipAndRootAccounting(t *testing.T) {
	t.Parallel()
	ctx := WithOwnership(t.Context(), t.TempDir(), []string{"src"})
	ctx = WithScope(ctx, "parent", "task")
	scope := GetScope(ctx, "child")
	require.Equal(t, "parent", scope.SessionID)
	require.NotEmpty(t, scope.WriteRoot)
	require.Equal(t, []string{"src"}, scope.OwnedPaths)
	s := NewStore(t.TempDir())
	require.NoError(t, s.SetBudget(ctx, "parent", "", Limits{MaxTokens: 10}))
	require.NoError(t, s.Charge(ctx, scope.SessionID, scope.TaskID, 10, 0.1, 0))
	require.ErrorContains(t, s.Check(ctx, "parent", "other-task"), "exhausted")
	require.NoError(t, s.SetBudget(ctx, "parent", "", Limits{MaxTokens: 20}))
	require.NoError(t, s.Check(ctx, "parent", "task"))
	state, err := s.Read(ctx, "parent")
	require.NoError(t, err)
	require.EqualValues(t, 10, state.Usage.Tokens)
	require.EqualValues(t, 10, state.Tasks["task"].Usage.Tokens)
}

func TestExplicitRecoveryRequiresEvidenceAndCannotReplay(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	id, err := s.Begin(t.Context(), "session", "call", "bash", `{}`, "")
	require.NoError(t, err)
	require.Error(t, s.Resolve(t.Context(), "session", id, "completed", ""))
	require.NoError(t, s.Resolve(t.Context(), "session", id, "abandoned", "Process no longer exists; inspected working tree"))
	require.Error(t, s.Resolve(t.Context(), "session", id, "completed", "Cannot rewrite resolved operation"))
	state, err := s.Read(t.Context(), "session")
	require.NoError(t, err)
	require.Equal(t, "abandoned", state.Operations[0].Status)
	require.Positive(t, state.Operations[0].FinishedAt)
	require.EqualValues(t, 1, state.Usage.ToolCalls)
}

func TestBudgetAndCanonicalFailureCircuit(t *testing.T) {
	s := NewStore(t.TempDir())
	require.NoError(t, s.Update(t.Context(), "s", func(st *State) error {
		st.Limits.MaxToolCalls = 3
		return nil
	}))
	for range 3 {
		id, err := s.Begin(t.Context(), "s", "c", "bash", `{"b":2,"a":1}`, "")
		require.NoError(t, err)
		require.NoError(t, s.Finish(t.Context(), "s", id, false, false))
	}
	_, err := s.Begin(t.Context(), "s", "c", "bash", `{"a":1,"b":2}`, "")
	require.Error(t, err)
	require.NoError(t, s.Update(t.Context(), "s", func(st *State) error { st.Limits.MaxToolCalls = 0; return nil }))
	_, err = s.Begin(t.Context(), "s", "c", "bash", `{"a":1,"b":2}`, "")
	require.ErrorContains(t, err, "Change the approach")
	id, err := s.Begin(t.Context(), "s", "edit", "edit", `{}`, "")
	require.NoError(t, err)
	require.NoError(t, s.Finish(t.Context(), "s", id, true, true))
	_, err = s.Begin(t.Context(), "s", "c", "bash", `{"a":1,"b":2}`, "")
	require.NoError(t, err)
}

func TestConcurrentStoresPreserveUpdatesAndCancellation(t *testing.T) {
	dir := t.TempDir()
	a, b := NewStore(dir), NewStore(dir)
	done := make(chan error, 2)
	for _, s := range []*Store{a, b} {
		go func() { done <- s.Charge(t.Context(), "s", "", 12, 0.2, time.Second) }()
	}
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	st, err := a.Read(t.Context(), "s")
	require.NoError(t, err)
	require.EqualValues(t, 24, st.Usage.Tokens)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, a.Update(ctx, "s", func(*State) error { return nil }), context.Canceled)
}

func TestFailureCircuitIgnoresDescriptionsAndKeepsTaskFailuresSeparate(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	for range 3 {
		id, err := s.Begin(t.Context(), "s", "c", "bash", `{"command":"false","description":"first label"}`, "a")
		require.NoError(t, err)
		require.NoError(t, s.Finish(t.Context(), "s", id, false, false))
	}
	_, err := s.Begin(t.Context(), "s", "c", "bash", `{"description":"new label","command":"false"}`, "a")
	require.ErrorContains(t, err, "Change the approach")
	_, err = s.Begin(t.Context(), "s", "c", "bash", `{"command":"false"}`, "b")
	require.NoError(t, err)
	id, err := s.Begin(t.Context(), "s", "edit", "edit", `{}`, "a")
	require.NoError(t, err)
	require.NoError(t, s.Finish(t.Context(), "s", id, true, true))
	state, err := s.Read(t.Context(), "s")
	require.NoError(t, err)
	require.Empty(t, state.Failures)
	require.EqualValues(t, 2, state.Usage.RepeatedFailedCalls)
	require.EqualValues(t, 2, state.Tasks["a"].Usage.RepeatedFailedCalls)
}
