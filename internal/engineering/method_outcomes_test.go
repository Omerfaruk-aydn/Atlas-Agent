package engineering

import (
	"fmt"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/stretchr/testify/require"
)

func TestSuccessfulMethodOutcomeRequiresAggregateAndTaskIdentity(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	root := t.TempDir()
	ctx := t.Context()
	k, err := methodKey(root)
	require.NoError(t, err)
	require.NoError(t, s.Update(ctx, k, func(st *State) error {
		st.LearnedMethods = []LearnedMethod{{ID: "method", Name: "measured"}}
		return nil
	}))
	stamp, _, err := s.WorkflowRecords(ctx, root, "task")
	require.NoError(t, err)
	source, err := ProjectFingerprint(ctx, root, s.Dir())
	require.NoError(t, err)
	tasks := Hash("current task specification")
	require.NoError(t, s.Update(ctx, "task", func(st *State) error {
		st.Work = &WorkState{Items: []WorkItem{{ID: "R1", Origin: OriginUser, Status: ItemVerified, Evidence: []EvidenceRef{{Passed: true, Generation: st.Generation}}}}}
		st.Completion = &CompletionCheckpoint{Verdict: "complete", Root: root, Revision: st.Revision + 1, Generation: st.Generation, RecordsFingerprint: stamp, SourceFingerprint: source, TasksFingerprint: tasks}
		return nil
	}))
	_, err = s.RecordMethodOutcome(ctx, root, "method", "task", true)
	require.ErrorContains(t, err, "task-service fingerprint")
	_, err = s.RecordMethodOutcome(ctx, root, "method", "task", true, Hash("different task"))
	require.ErrorContains(t, err, "task-service fingerprint")
	m, err := s.RecordMethodOutcome(ctx, root, "method", "task", true, tasks)
	require.NoError(t, err)
	require.Len(t, m.Outcomes, 1)
	require.True(t, m.Outcomes[0].Success)
	require.NoError(t, s.Update(ctx, "task", func(st *State) error {
		st.Outcomes.Record(outcome.Record{OpID: "unknown", Mutation: true, Phase: outcome.PhaseUnknown})
		return nil
	}))
	_, err = s.RecordMethodOutcome(ctx, root, "method", "task", true, tasks)
	require.ErrorContains(t, err, "aggregate completion", "local item evidence cannot hide an unknown effect")
}

func outcomes(results ...bool) LearnedMethod {
	var m LearnedMethod
	for i, ok := range results {
		m.Outcomes = append(m.Outcomes, MethodOutcome{TaskID: fmt.Sprintf("task-%d", i), Success: ok})
	}
	return m
}

func TestMethodStandingNeedsSeveralTasks(t *testing.T) {
	t.Parallel()

	standing, _ := MethodStanding(LearnedMethod{})
	require.Equal(t, StandingCandidate, standing)

	// One task succeeding a hundred times is still one task.
	var repeated LearnedMethod
	for range 100 {
		repeated.Outcomes = append(repeated.Outcomes, MethodOutcome{TaskID: "same", Success: true})
	}
	standing, reason := MethodStanding(repeated)
	require.Equal(t, StandingCandidate, standing, reason)

	standing, _ = MethodStanding(outcomes(true, true))
	require.Equal(t, StandingCandidate, standing)
	standing, _ = MethodStanding(outcomes(true, true, true))
	require.Equal(t, StandingEffective, standing)
	standing, _ = MethodStanding(outcomes(true, false, false))
	require.Equal(t, StandingUnreliable, standing)
	standing, _ = MethodStanding(outcomes(true, true, true, false))
	require.Equal(t, StandingEffective, standing, "3 of 4 meets the threshold")
}

func TestLaterOutcomeOfATaskReplacesItsEarlierOne(t *testing.T) {
	t.Parallel()

	m := outcomes(true, true, true)
	m.Outcomes = append(m.Outcomes, MethodOutcome{TaskID: "task-0", Success: false}, MethodOutcome{TaskID: "task-1", Success: false})
	standing, _ := MethodStanding(m)
	require.Equal(t, StandingUnreliable, standing)
}

func TestSuccessNeedsVerifiedRequirementsOfThatTask(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	ctx := t.Context()
	root := t.TempDir()
	k, err := methodKey(root)
	require.NoError(t, err)
	require.NoError(t, s.Update(ctx, k, func(st *State) error {
		st.LearnedMethods = append(st.LearnedMethods, LearnedMethod{ID: "method-1", Name: "m"})
		return nil
	}))

	_, err = s.RecordMethodOutcome(ctx, root, "method-1", "task-a", true)
	require.ErrorContains(t, err, "work contract", "a success with no contract is only a claim")

	now := time.UnixMilli(1000)
	_, err = s.UpdateWork(ctx, "task-a", func(w *WorkState, st State) error {
		turn := w.RecordUserTurn("fix the parser", now)
		_, err := w.AddItem("behavior", "parser accepts empty input", OriginUser, []int{turn.Seq}, now)
		return err
	})
	require.NoError(t, err)
	_, err = s.RecordMethodOutcome(ctx, root, "method-1", "task-a", true)
	require.ErrorContains(t, err, "verified", "an open requirement cannot back a success")

	// A failure never needs proof.
	m, err := s.RecordMethodOutcome(ctx, root, "method-1", "task-a", false)
	require.NoError(t, err)
	require.Len(t, m.Outcomes, 1)

	_, err = s.RecordMethodOutcome(ctx, root, "nope", "task-a", false)
	require.ErrorContains(t, err, "unknown learned method")
}

func TestAnUnreliableMethodIsNotExported(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	ctx := t.Context()
	root := t.TempDir()
	k, err := methodKey(root)
	require.NoError(t, err)
	bad := outcomes(true, false, false)
	bad.ID, bad.Name = "method-bad", "bad"
	fresh := LearnedMethod{ID: "method-new", Name: "new"}
	require.NoError(t, s.Update(ctx, k, func(st *State) error {
		st.LearnedMethods = append(st.LearnedMethods, bad, fresh)
		return nil
	}))

	require.ErrorContains(t, s.MarkMethodExported(ctx, root, "method-bad", "x/SKILL.md"), "unreliable")
	require.NoError(t, s.MarkMethodExported(ctx, root, "method-new", "x/SKILL.md"), "an unmeasured candidate may still be exported, labelled as such")
	require.Contains(t, MethodInstructions(fresh), "Standing: candidate")
}

func TestReusableMethodsAreRankedByStandingAndExcludeUnreliable(t *testing.T) {
	t.Parallel()

	proven := outcomes(true, true, true)
	proven.ID, proven.Name = "proven", "proven"
	untried := LearnedMethod{ID: "untried", Name: "untried"}
	failing := outcomes(true, false, false)
	failing.ID, failing.Name = "failing", "failing"
	newer := LearnedMethod{ID: "newer", Name: "newer"}

	// Learned order: proven, untried, failing, newer.
	choices := ReusableMethods([]LearnedMethod{proven, untried, failing, newer}, 8)
	var ids []string
	for _, c := range choices {
		ids = append(ids, c.ID)
	}
	require.Equal(t, []string{"proven", "newer", "untried"}, ids, "the measured method leads, candidates follow newest first, the unreliable one is not offered")
	require.Equal(t, StandingEffective, choices[0].Standing)
	require.Contains(t, choices[0].Guidance, "prefer it")
	require.Equal(t, StandingCandidate, choices[1].Standing)
	require.Contains(t, choices[1].Guidance, "Not yet measured")

	require.Len(t, ReusableMethods([]LearnedMethod{proven, untried, newer}, 2), 2, "the limit applies after ranking, so a proven method is never cut for a newer candidate")
	require.Equal(t, "proven", ReusableMethods([]LearnedMethod{proven, untried, newer}, 1)[0].ID)
}

func TestExportingAnEffectiveMethodMarksItEstablished(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	ctx := t.Context()
	root := t.TempDir()
	k, err := methodKey(root)
	require.NoError(t, err)
	proven := outcomes(true, true, true)
	proven.ID, proven.Name = "method-ok", "ok"
	fresh := LearnedMethod{ID: "method-new", Name: "new"}
	// These catalog fixtures have observed extraction provenance; evidence-free
	// legacy entries are now removed before they can be offered as guidance.
	for _, m := range []*LearnedMethod{&proven, &fresh} {
		m.SessionID = "origin"
		m.CreatedAt = time.Now().UnixMilli()
		m.Evidence = []EvidenceRef{{OperationID: "observed-check", Passed: true, ObservedAt: 1}}
	}
	require.NoError(t, s.Update(ctx, k, func(st *State) error {
		st.LearnedMethods = append(st.LearnedMethods, proven, fresh)
		return nil
	}))
	require.NoError(t, s.MarkMethodExported(ctx, root, "method-ok", "x/SKILL.md"))
	require.NoError(t, s.MarkMethodExported(ctx, root, "method-new", "y/SKILL.md"))
	methods, err := s.LearnedMethods(ctx, root)
	require.NoError(t, err)
	require.Equal(t, "exported", methods[0].Status)
	require.Equal(t, "exported_candidate", methods[1].Status)
}
