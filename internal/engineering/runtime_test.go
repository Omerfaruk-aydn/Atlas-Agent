package engineering

import (
	"context"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
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

func TestRecoveryResolvesCanceledAndFailedUnknownOutcomes(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"running", "failed"} {
		for resolution, phase := range map[string]outcome.Phase{"completed": outcome.PhaseApplied, "failed": outcome.PhaseFailed, "abandoned": outcome.PhaseCanceled} {
			t.Run(status+"/"+resolution, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				store := NewStore(dir)
				id, err := store.Begin(t.Context(), "session", "call", "mcp_write", `{}`, "")
				require.NoError(t, err)
				require.NoError(t, store.Update(t.Context(), "session", func(st *State) error {
					st.Operations[0].Status = status
					st.Outcomes.Record(outcome.Record{OpID: id, Tool: "mcp_write", Phase: outcome.PhaseUnknown, Mutation: true, Subjects: []string{"*"}, Fingerprint: Hash("call")})
					st.Outcomes.Record(outcome.Record{OpID: "known", Tool: "view", Phase: outcome.PhaseVerified})
					st.Outcomes.Add(outcome.Evidence{ID: "prior", Kind: outcome.KindOutput, Source: "check", Subject: "workspace", Claim: "passed", StaleBy: id})
					return nil
				}))
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				inspection := "Inspected remote state after interrupted dispatch"
				require.ErrorIs(t, store.Resolve(ctx, "session", id, resolution, inspection), context.Canceled)
				before, err := store.Read(t.Context(), "session")
				require.NoError(t, err)
				require.Equal(t, outcome.PhaseUnknown, before.Outcomes.Records[0].Phase)
				restarted := NewStore(dir)
				require.NoError(t, restarted.Resolve(t.Context(), "session", id, resolution, inspection))
				state, err := NewStore(dir).Read(t.Context(), "session")
				require.NoError(t, err)
				require.Equal(t, resolution, state.Operations[0].Status)
				require.Equal(t, Hash(inspection), state.Operations[0].EvidenceHash)
				require.True(t, state.Operations[0].OutcomeObserved)
				require.Equal(t, phase, state.Outcomes.Records[0].Phase)
				require.Equal(t, before.Outcomes.Records[1], state.Outcomes.Records[1])
				require.Empty(t, state.Outcomes.Unresolved())
				require.Equal(t, before.Outcomes.Evidence[0], state.Outcomes.Evidence[0])
				require.True(t, state.Outcomes.Supports(outcome.KindObservation, id, inspection))
				require.ErrorContains(t, restarted.Resolve(t.Context(), "session", id, "completed", inspection), "unresolved operation not found")
			})
		}
	}
}

func TestOperationCompactionRetainsFailedUnknownOutcomes(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	require.NoError(t, store.Update(t.Context(), "session", func(st *State) error {
		st.Operations = append(st.Operations, Operation{ID: "unknown", Status: "failed"})
		st.Outcomes.Record(outcome.Record{OpID: "unknown", Phase: outcome.PhaseUnknown, Mutation: true})
		for range 255 {
			st.Operations = append(st.Operations, Operation{Status: "completed"})
		}
		return nil
	}))
	_, err := store.Begin(t.Context(), "session", "next", "view", `{}`, "")
	require.NoError(t, err)
	state, err := store.Read(t.Context(), "session")
	require.NoError(t, err)
	require.Equal(t, "unknown", state.Operations[0].ID)
	require.NoError(t, store.Resolve(t.Context(), "session", "unknown", "abandoned", "Inspected the actual remote effects"))
}

func TestRecoveryResolvesLegacyLedgerOnlyUnknown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	require.NoError(t, store.Update(t.Context(), "session", func(st *State) error {
		st.Outcomes.Record(outcome.Record{OpID: "evicted", Tool: "mcp_write", Phase: outcome.PhaseUnknown, Mutation: true})
		return nil
	}))
	require.Error(t, store.Resolve(t.Context(), "session", "evicted", "completed", ""))
	require.NoError(t, NewStore(dir).Resolve(t.Context(), "session", "evicted", "completed", "Inspected the saved remote artifact"))
	state, err := NewStore(dir).Read(t.Context(), "session")
	require.NoError(t, err)
	require.Empty(t, state.Operations)
	require.Empty(t, state.Outcomes.Unresolved())
	require.Equal(t, outcome.PhaseApplied, state.Outcomes.Records[0].Phase)
	require.True(t, state.Outcomes.Supports(outcome.KindObservation, "evicted", "Inspected the saved remote artifact"))
	require.Error(t, store.Resolve(t.Context(), "session", "evicted", "failed", "Cannot rewrite inspected resolution"))
}

func TestRecoveryResolvesDispatchedBackgroundInBothJournals(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	id, err := store.Begin(t.Context(), "session", "job", "bash", `{}`, "")
	require.NoError(t, err)
	require.NoError(t, store.Update(t.Context(), "session", func(st *State) error {
		st.Outcomes.Record(outcome.Record{OpID: id, Tool: "bash", Phase: outcome.PhaseDispatched, Mutation: true})
		return nil
	}))
	require.NoError(t, store.Resolve(t.Context(), "session", id, "completed", "Inspected the actual completed background artifact"))
	state, err := NewStore(dir).Read(t.Context(), "session")
	require.NoError(t, err)
	require.Len(t, state.Operations, 1)
	require.Len(t, state.Outcomes.Records, 1)
	require.Equal(t, "completed", state.Operations[0].Status)
	require.Equal(t, outcome.PhaseApplied, state.Outcomes.Records[0].Phase)
	require.True(t, state.Outcomes.Records[0].Resolved)
}
