package engineering

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func op(id, tool, status string, gen uint64, finished int64) Operation {
	return Operation{ID: id, CallID: "call-" + id, Tool: tool, Status: status, OutcomeObserved: true, Generation: gen, StartedAt: finished - 1, FinishedAt: finished}
}

func TestContractSeparatesUserRequestsFromProposals(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(1000)
	var w WorkState
	first := w.RecordUserTurn("Add CSV export to the report page", now)
	require.Equal(t, "request", first.Kind)
	cont := w.RecordUserTurn("devam et", now)
	require.Equal(t, "continuation", cont.Kind)
	fix := w.RecordUserTurn("Use semicolons, not commas", now)
	require.Equal(t, "follow_up", fix.Kind)

	_, err := w.AddItem("behavior", "Export as CSV", OriginUser, nil, now)
	require.ErrorContains(t, err, "source_turns")
	_, err = w.AddItem("behavior", "Export as CSV", OriginUser, []int{cont.Seq}, now)
	require.ErrorContains(t, err, "only says to continue")
	csv, err := w.AddItem("behavior", "Export as CSV", OriginUser, []int{first.Seq}, now)
	require.NoError(t, err)
	extra, err := w.AddItem("deliverable", "Also add XLSX export", OriginProposal, []int{first.Seq}, now)
	require.NoError(t, err)
	require.Empty(t, extra.SourceTurns, "a proposal never carries user provenance")

	require.ErrorContains(t, w.Decline(csv.ID, now), "cannot decline a user requirement")
	require.NoError(t, w.Decline(extra.ID, now))

	semi, err := w.AddItem("behavior", "Export with semicolon separator", OriginUser, []int{fix.Seq}, now)
	require.NoError(t, err)
	require.ErrorContains(t, w.Supersede(csv.ID, semi.ID, first.Seq, now), "later recorded user correction")
	require.NoError(t, w.Supersede(csv.ID, semi.ID, fix.Seq, now))

	view := w.View(State{})
	require.Len(t, view.Requested, 1)
	require.Equal(t, semi.ID, view.Requested[0].ID)
	require.Empty(t, view.Proposed)
	require.Equal(t, 2, view.Omitted["finished_items"])
}

func TestVerificationRequiresCurrentObservedCheck(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(5000)
	var w WorkState
	turn := w.RecordUserTurn("fix the parser", now)
	item, err := w.AddItem("behavior", "parser accepts empty input", OriginUser, []int{turn.Seq}, now)
	require.NoError(t, err)

	st := State{Generation: 2, Operations: []Operation{
		op("edit", "edit", "completed", 2, 100),
		op("old-test", "test_run", "completed", 1, 90),
		op("red", "test_run", "failed", 2, 110),
		{ID: "unobserved", Tool: "test_run", Status: "completed", Generation: 2},
		op("green", "test_run", "completed", 2, 120),
	}}
	require.ErrorContains(t, w.Advance(st, item.ID, ItemVerified, []string{"edit"}, now), "passing check")
	require.ErrorContains(t, w.Advance(st, item.ID, ItemVerified, []string{"old-test"}, now), "current source generation")
	require.ErrorContains(t, w.Advance(st, item.ID, ItemVerified, []string{"unobserved"}, now), "no observed outcome")
	require.ErrorContains(t, w.Advance(st, item.ID, ItemVerified, []string{"green", "red"}, now), "failing check cannot verify")
	require.ErrorContains(t, w.Advance(st, item.ID, ItemDelivered, []string{"green"}, now), "delivery is recorded by the runtime")
	require.NoError(t, w.Advance(st, item.ID, ItemImplemented, []string{"edit"}, now))
	// The model may cite the call ID it saw.
	require.NoError(t, w.Advance(st, item.ID, ItemVerified, []string{"call-green"}, now))
	require.Equal(t, ItemVerified, EffectiveStatus(*w.item(item.ID), 2))

	// A later change makes the verification stale without rewriting it,
	// and stale items are not marked delivered.
	require.Equal(t, ItemVerified+"_stale", EffectiveStatus(*w.item(item.ID), 3))
	w.MarkDelivered(3, now)
	require.Equal(t, ItemVerified, w.item(item.ID).Status)
	w.MarkDelivered(2, now)
	require.Equal(t, ItemDelivered, w.item(item.ID).Status)
}

func TestMethodChangesNeedNewEvidence(t *testing.T) {
	t.Parallel()

	st := State{Operations: []Operation{op("o1", "bash", "failed", 0, 50), op("o2", "bash", "failed", 0, 500)}}
	var w WorkState
	_, err := w.AddMethod(st, "direct", "small focused change", nil, "agent", time.UnixMilli(100))
	require.NoError(t, err)
	_, err = w.AddMethod(st, "experiment", "need to test", nil, "agent", time.UnixMilli(200))
	require.ErrorContains(t, err, "must cite the new evidence")
	_, err = w.AddMethod(st, "experiment", "need to test", []string{"o1"}, "agent", time.UnixMilli(200))
	require.ErrorContains(t, err, "predates the previous method decision")
	_, err = w.AddMethod(st, "experiment", "build failed twice", []string{"o2"}, "agent", time.UnixMilli(600))
	require.NoError(t, err)
	_, err = w.AddMethod(st, "unknown", "x", []string{"o2"}, "agent", time.UnixMilli(700))
	require.ErrorContains(t, err, "method must be one of")
}

func TestExperimentsUseFreshObservationsAndStateLimits(t *testing.T) {
	t.Parallel()

	var w WorkState
	h, err := w.AddHypothesis("timeouts come from DNS resolution", time.UnixMilli(10))
	require.NoError(t, err)
	_, err = w.AddExperiment(h.ID, "Does a fixed IP remove the timeout?", "timeout persists => refuted", false, "", time.UnixMilli(20))
	require.ErrorContains(t, err, "must state its limits")
	e, err := w.AddExperiment(h.ID, "Does a fixed IP remove the timeout?", "timeout persists => refuted", false, "local stub server, not the production resolver", time.UnixMilli(100))
	require.NoError(t, err)

	st := State{Operations: []Operation{op("before", "bash", "completed", 0, 50), op("after", "bash", "completed", 0, 150)}}
	require.ErrorContains(t, w.RecordExperiment(st, e.ID, "refuted", "still times out", []string{"before"}), "before the experiment was registered")
	require.NoError(t, w.RecordExperiment(st, e.ID, "refuted", "still times out", []string{"after"}))
	require.ErrorContains(t, w.RecordExperiment(st, e.ID, "supported", "x", []string{"after"}), "already has an outcome")
	require.Equal(t, "refuted", w.hypothesis(h.ID).Status)
	require.True(t, w.hypothesis(h.ID).Limited, "a non-representative fixture only gives a limited conclusion")
}

func TestCandidatesAreDistinctAndSelectedByMeasuredSuite(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(1000)
	w := WorkState{Budget: WorkBudget{Tier: TierDirect}}
	_, err := w.AddCandidate(State{}, "binary search over sorted index", "algorithm", "", "", now)
	require.ErrorContains(t, err, "allows 0 candidates", "simple tasks never get forced alternatives")

	w.Budget.Tier = TierStandard
	a, err := w.AddCandidate(State{}, "binary search over the sorted index", "algorithm", "", "", now)
	require.NoError(t, err)
	_, err = w.AddCandidate(State{}, "Binary search over sorted index!", "algorithm", "", "", now)
	require.ErrorContains(t, err, "restatement")
	b, err := w.AddCandidate(State{}, "hash map keyed by normalized name", "algorithm", "", "", now)
	require.NoError(t, err)

	_, err = w.Select(a.ID, "", now)
	require.ErrorContains(t, err, "no discriminating suite")
	require.NoError(t, w.SetSuite([]string{"unicode names", "duplicate keys"}, "inputs where the approaches differ", now))

	st := State{Generation: 1, Operations: []Operation{op("va", "verify", "completed", 1, 10), op("vb", "verify", "failed", 2, 20), op("vb2", "verify", "completed", 1, 30)}, Checks: []Check{
		{RunID: "call-va", Name: "unicode names", Passed: true},
		{RunID: "call-va", Name: "duplicate keys", Passed: true},
		{RunID: "call-vb", Name: "unicode names", Passed: true},
		{RunID: "call-vb", Name: "duplicate keys", Passed: false},
		{RunID: "call-vb2", Name: "unicode names", Passed: true},
		{RunID: "call-vb2", Name: "duplicate keys", Passed: true},
	}}
	require.NoError(t, w.RecordCandidateResult(st, a.ID, "va", now))
	require.NotZero(t, w.Suite.LockedAt)
	// One run cannot stand for two candidates in a shared checkout.
	require.ErrorContains(t, w.RecordCandidateResult(st, b.ID, "vb2", now), "same source generation")
	require.NoError(t, w.RecordCandidateResult(st, b.ID, "vb", now))

	_, err = w.Select(b.ID, "", now)
	require.ErrorContains(t, err, "did not pass the complete suite")
	basis, err := w.Select(a.ID, "", now)
	require.NoError(t, err)
	require.Contains(t, basis, "only candidate")

	// Weakening the suite after results is visible and resets every result.
	require.NoError(t, w.SetSuite([]string{"unicode names"}, "duplicate keys judged out of scope", now))
	require.Len(t, w.Suite.Revisions, 1)
	require.Contains(t, w.Suite.Revisions[0], "removed: duplicate keys")
	for _, c := range w.Candidates {
		require.Empty(t, c.Results)
	}
}

func TestBudgetStartsFromRequestAndOnlyRises(t *testing.T) {
	t.Parallel()

	require.Equal(t, TierDirect, ClassifyBudget("What does the summarize threshold do?", nil, "").Tier)
	require.Equal(t, TierStandard, ClassifyBudget("Fix the off-by-one in pagination", nil, "small_fix").Tier)
	deep := ClassifyBudget("Tests fail intermittently on CI; find the root cause", nil, "")
	require.Equal(t, TierDeep, deep.Tier)
	require.NotEmpty(t, deep.Reasons)

	w := &WorkState{Budget: WorkBudget{Tier: TierDeep}}
	require.Equal(t, TierDeep, ClassifyBudget("what is this?", w, "").Tier, "never drops within a task")
	w = &WorkState{Hypotheses: []Hypothesis{{Status: "refuted"}}}
	require.Equal(t, TierStandard, ClassifyBudget("why is this slow?", w, "").Tier)
	w = &WorkState{Budget: WorkBudget{Override: TierDirect}, Hypotheses: []Hypothesis{{Status: "refuted"}}}
	require.Equal(t, TierDirect, ClassifyBudget("refactor the architecture", w, "").Tier, "the user's setting wins")
}

func TestQuestionsNeedUserOrObservedAnswers(t *testing.T) {
	t.Parallel()

	var w WorkState
	q, err := w.AddQuestion("Which date format do reports use?", nil, time.UnixMilli(1))
	require.NoError(t, err)
	require.ErrorContains(t, w.AnswerQuestion(State{}, q.ID, "ISO", 0, nil), "user turn or observed operations")
	turn := w.RecordUserTurn("use ISO 8601", time.UnixMilli(2))
	require.NoError(t, w.AnswerQuestion(State{}, q.ID, "ISO 8601", turn.Seq, nil))
	require.Empty(t, w.View(State{}).Questions)
}

func TestCorruptStateIsQuarantinedNotFatal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	ctx := t.Context()
	require.NoError(t, store.Update(ctx, "s", func(st *State) error { st.Usage.ToolCalls = 3; return nil }))
	path := filepath.Join(store.Dir(), Hash("s")+".json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":1,"operations":[{"id":`), 0o600))

	st, err := store.Read(ctx, "s")
	require.NoError(t, err)
	require.NotNil(t, st.Recovered)
	require.Zero(t, st.Usage.ToolCalls)
	data, err := os.ReadFile(st.Recovered.QuarantinedPath)
	require.NoError(t, err)
	require.Contains(t, string(data), `"operations"`, "the damaged record is kept for inspection")

	// The session keeps working after recovery.
	_, err = store.UpdateWork(ctx, "s", func(w *WorkState, _ State) error {
		w.RecordUserTurn("continue", time.Now())
		return nil
	})
	require.NoError(t, err)
	st, err = store.Read(ctx, "s")
	require.NoError(t, err)
	require.NotNil(t, st.Recovered)
	require.Len(t, st.Work.Turns, 1)
}

func TestConcurrentWorkUpdatesAreSerialized(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	ctx := t.Context()
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			_, err := store.UpdateWork(ctx, "s", func(w *WorkState, _ State) error {
				_, err := w.AddHypothesis("h", time.UnixMilli(int64(i)))
				if err != nil && w.Seq >= maxWorkHypotheses {
					return nil
				}
				return err
			})
			require.NoError(t, err)
		})
	}
	wg.Wait()
	st, err := store.Read(ctx, "s")
	require.NoError(t, err)
	require.Len(t, st.Work.Hypotheses, maxWorkHypotheses)
	seen := map[string]bool{}
	for _, h := range st.Work.Hypotheses {
		require.False(t, seen[h.ID], "duplicate id %s", h.ID)
		seen[h.ID] = true
	}
}
