package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

// stands reports whether an evaluation would let the work stand as complete.
func stands(e evaluatorDecision) bool {
	return e.Continue == "" && e.Unresolved == "" && e.Unverified == ""
}

// The catalogue: for each situation, what the legacy paths conclude, what the
// unified decision concludes, and whether they differ. A difference where the
// legacy path is the more permissive one is a claim the binding withholds; a
// difference the other way is kept (the legacy path stays more conservative).
func TestLegacyAndUnifiedDecisionCatalogue(t *testing.T) {
	t.Parallel()

	verified := []outcome.Requirement{req("a", outcome.PhaseVerified)}
	cases := []struct {
		name           string
		evaluation     evaluatorDecision
		input          unifiedInput
		legacyComplete bool
		unified        completionVerdict
	}{
		{"agreement: verified and reviewed", evaluatorDecision{Review: reviewPassed}, unifiedInput{Requirements: verified, Review: reviewPassed}, true, completeVerdict},
		{"legacy overclaims: review could not run", evaluatorDecision{Review: reviewUnavailable}, unifiedInput{Requirements: verified, Review: reviewUnavailable}, true, unverifiedVerdict},
		{"legacy overclaims: open finding the evaluator never saw", evaluatorDecision{}, unifiedInput{Requirements: verified, OpenFindings: []string{"stale read"}}, true, incompleteVerdict},
		{"legacy overclaims: required check missing", evaluatorDecision{}, unifiedInput{Requirements: verified, ContractGaps: []string{"go test: not run"}}, true, incompleteVerdict},
		{"legacy overclaims: unobserved effect", evaluatorDecision{}, unifiedInput{Requirements: verified, UnknownEffects: 1}, true, blockedVerdict},
		{"legacy overclaims: paused", evaluatorDecision{}, unifiedInput{Requirements: verified, Paused: true}, true, blockedVerdict},
		{"agreement: evaluator sent it back", evaluatorDecision{Continue: "fix"}, unifiedInput{Requirements: verified, OpenFindings: []string{"fix"}}, false, incompleteVerdict},
		{"agreement: applied only", evaluatorDecision{}, unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseApplied)}}, false, unverifiedVerdict},
		{"unified is the more permissive one: a waived review", evaluatorDecision{Review: reviewWaived, Unresolved: "x"}, unifiedInput{Requirements: verified, Review: reviewWaived}, false, completeVerdict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.legacyComplete, legacyComplete(tc.evaluation, tc.input.Requirements))
			decision := decideCompletion(tc.input)
			require.Equal(t, tc.unified, decision.Verdict, "%v", decision.Reasons)

			bound := bindCompletion(tc.evaluation, decision)
			if !decision.Complete() {
				require.False(t, stands(bound), "no path may stand as complete while the unified decision is %s", decision.Verdict)
			}
			if tc.legacyComplete && !decision.Complete() {
				require.NotEmpty(t, bound.Unverified, "the withheld claim carries its reasons")
			}
			if !stands(tc.evaluation) {
				require.Equal(t, tc.evaluation, bound, "binding never loosens a path that already withholds completion")
			}
		})
	}
}

func appliedTodoSession(t *testing.T, env fakeEnv) session.Session {
	t.Helper()
	sess, err := env.sessions.Create(t.Context(), "binding")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "t1", Content: "Implement", Status: session.TodoStatusCompleted}}
	_, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	return sess
}

func openFindingFor(t *testing.T, env fakeEnv, s *engineering.Store, sess session.Session) {
	t.Helper()
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("broken"), 0o600))
	task := sess.Todos[0]
	fp, err := engineering.SourceFingerprint(t.Context(), root, s.Dir())
	require.NoError(t, err)
	f := engineering.Finding{Root: root, TaskID: task.ID, TaskFingerprint: session.TaskFingerprint(task), ReviewerExecutionID: "review", ImplementerExecutionID: "impl", SourceFingerprint: fp, Status: "open", Path: "api.go", StartLine: 1, EndLine: 1, Severity: 2, Issue: "Empty result", Expected: "Return value", Evidence: "Observed"}
	f.ID = engineering.FindingID(f)
	require.NoError(t, s.Update(t.Context(), sess.ID, func(st *engineering.State) error {
		st.Operations = []engineering.Operation{{ID: "review", Tool: "agent", AgentName: "review", TaskID: task.ID, Status: "completed"}, {ID: "impl", Tool: "agent", AgentName: "backend", TaskID: task.ID, Status: "completed"}}
		return nil
	}))
	_, err = s.SaveFinding(t.Context(), sess.ID, f, 0)
	require.NoError(t, err)
}

// The goal loop took the agent's claim and a judge's agreement as enough. A
// requirement without proof or an open finding now holds the goal open.
func TestGoalClaimCannotOutrankRuntimeBlockers(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	s := engineering.NewStore(t.TempDir())
	sess := appliedTodoSession(t, env)

	blockers := completionBlockers(t.Context(), s, env.sessions, sess.ID)
	require.NotEmpty(t, blockers, "applied but unverified work blocks a verified completion")
	require.Contains(t, blockers[0], "applied but not verified")

	openFindingFor(t, env, s, sess)
	blockers = completionBlockers(t.Context(), s, env.sessions, sess.ID)
	require.Contains(t, blockers, "1 open finding(s)")

	// Nothing planned: nothing to hold the goal.
	empty, err := env.sessions.Create(t.Context(), "empty")
	require.NoError(t, err)
	require.Empty(t, completionBlockers(t.Context(), s, env.sessions, empty.ID))
}

// An open finding keeps a green contract from relabelling applied work as
// verified, and keeps an answer from being recorded as a delivery.
func TestOpenFindingBlocksContractRelabelAndDelivery(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	s := engineering.NewStore(t.TempDir())
	sess := appliedTodoSession(t, env)

	a := newRootAgent(t.TempDir())
	a.engineering = s
	ledger := a.evaluatorLedgerFor(sess.ID)
	ledger.lastEdit = 2
	ledger.greenEdit = ledger.lastEdit + 1
	applied := func() []outcome.Requirement { return []outcome.Requirement{{ID: "t1", Phase: outcome.PhaseApplied}} }
	require.Equal(t, outcome.PhaseVerified, a.verifiedByContract(sess.ID, applied())[0].Phase, "without findings the green contract still supports applied work")

	openFindingFor(t, env, s, sess)
	require.Equal(t, outcome.PhaseApplied, a.verifiedByContract(sess.ID, applied())[0].Phase, "an open finding stands against it")

	require.NoError(t, s.Update(context.Background(), sess.ID, func(st *engineering.State) error {
		st.Work = &engineering.WorkState{Items: []engineering.WorkItem{{
			ID: "W1", Status: engineering.ItemVerified,
			Evidence: []engineering.EvidenceRef{{OperationID: "op", Passed: true, Generation: st.Generation}},
		}}}
		return nil
	}))
	a.sessions = env.sessions
	evaluation, _ := a.decideTurnCompletion(t.Context(), sess.ID, evaluatorDecision{})
	require.NotEmpty(t, evaluation.Unverified)
	st, err := s.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, engineering.ItemVerified, st.Work.Items[0].Status, "an open finding keeps verified work from being recorded as delivered")
}

func stateJSON(t *testing.T, s *engineering.Store, id string) string {
	t.Helper()
	st, err := s.Read(t.Context(), id)
	require.NoError(t, err)
	data, err := json.Marshal(st)
	require.NoError(t, err)
	return string(data)
}

// The runtime persists its withheld aggregate decision without promoting
// item evidence to delivery. Repeating it has the same semantic result.
func TestUnifiedDecisionPersistsWithoutAdvancingDelivery(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	s := engineering.NewStore(t.TempDir())
	sess := appliedTodoSession(t, env)
	openFindingFor(t, env, s, sess)
	a := newRootAgent(t.TempDir())
	a.engineering, a.sessions = s, env.sessions

	first, _ := a.decideTurnCompletion(t.Context(), sess.ID, evaluatorDecision{})
	second, _ := a.decideTurnCompletion(t.Context(), sess.ID, evaluatorDecision{})
	require.NotEmpty(t, first.Unverified, "applied work with an open finding is withheld")
	require.Equal(t, first, second, "the same facts give the same decision")

	// The withheld claim must not have been recorded as a delivery.
	st, err := s.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.NotNil(t, st.Completion)
	require.Equal(t, "incomplete", st.Completion.Verdict)
	if st.Work != nil {
		for _, item := range st.Work.Items {
			require.NotEqual(t, engineering.ItemDelivered, item.Status)
		}
	}
}

// The tree changes after the decision was made and before it is used. The
// decision is made again against the new tree; a tree that never settles is
// not reported as complete.
func TestTreeChangingDuringTheDecisionIsNotReportedComplete(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	s := engineering.NewStore(t.TempDir())
	sess := appliedTodoSession(t, env)
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package a\n"), 0o644))
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	passingCheck(t, s, ctx, root)

	a := newRootAgent(root)
	a.engineering, a.sessions = s, env.sessions
	generationOf := func() uint64 {
		st, err := s.Read(ctx, sess.ID)
		require.NoError(t, err)
		return st.Generation
	}
	generation := generationOf()

	writes := 0
	a.beforeDriftRecheck = func() {
		writes++
		require.NoError(t, os.WriteFile(file, []byte(fmt.Sprintf("package a%d\n", writes)), 0o644))
	}
	evaluation, _ := a.decideTurnCompletion(ctx, sess.ID, evaluatorDecision{})
	require.Greater(t, writes, 1, "the decision was retried against the changed tree")
	require.Greater(t, generationOf(), generation, "each late change made the earlier check stale")
	require.NotEmpty(t, evaluation.Unverified, "a tree that kept changing is never reported as verified")

	// Once it stops changing the decision stands on the current tree.
	a.beforeDriftRecheck = nil
	again, _ := a.decideTurnCompletion(ctx, sess.ID, evaluatorDecision{})
	require.NotContains(t, again.Unverified, "kept changing")
}
