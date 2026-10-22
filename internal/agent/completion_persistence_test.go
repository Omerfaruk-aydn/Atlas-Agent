package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestRunNeverDeliversVerifiedItemsBeforeTheReviewerAccepts(t *testing.T) {
	t.Parallel()
	var store *engineering.Store
	register := fantasy.NewAgentTool("workflow", "Record observed local verification.", func(ctx context.Context, _ probeInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
		_, err := store.UpdateWork(ctx, tools.GetSessionFromContext(ctx), func(w *engineering.WorkState, st engineering.State) error {
			w.Items = []engineering.WorkItem{{ID: "R1", Kind: "behavior", Text: "cache works", Origin: engineering.OriginUser, SourceTurns: []int{1}, Status: engineering.ItemVerified, Evidence: []engineering.EvidenceRef{{OperationID: "local-check", Passed: true, Generation: st.Generation}}}}
			return nil
		})
		return fantasy.NewTextResponse("local check passed"), err
	})
	model := &scriptedModel{steps: []scriptedStep{
		{tool: "edit", input: `{"file_path":"a.go"}`},
		{tool: "workflow", input: `{"port":1}`},
		{text: "Done."},
		{tool: "edit", input: `{"file_path":"a.go"}`},
		{tool: "workflow", input: `{"port":1}`},
		{text: "Done again."},
	}}
	a, env, created := newWorkLoopAgent(t, model, fakeEditTool(), register)
	store = created
	reviewer := &scriptedModel{repeat: &scriptedStep{text: "VERDICT: FAIL\n- a.go:3 the acceptance criterion still fails"}}
	withEvaluator(a, reviewer, 1)
	sess, err := env.sessions.Create(t.Context(), "review withheld")
	require.NoError(t, err)
	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "fix cache", NonInteractive: true, MaxOutputTokens: 1000})
	require.NoError(t, err)
	require.NotEmpty(t, reviewer.calls(), "the test must execute the reviewer after the answer")
	st, err := store.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, engineering.ItemVerified, st.Work.Items[0].Status, "sending the answer before review never promotes local evidence to delivery")
	require.NotEqual(t, "complete", st.Completion.Verdict)
	require.NotEqual(t, "deliver", st.Work.Phase)
}

func verifiedWorkSession(t *testing.T) (*sessionAgent, string) {
	t.Helper()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "verified work")
	require.NoError(t, err)
	a := newRootAgent(t.TempDir())
	a.sessions, a.engineering = env.sessions, engineering.NewStore(t.TempDir())
	_, err = a.engineering.UpdateWork(t.Context(), sess.ID, func(w *engineering.WorkState, st engineering.State) error {
		turn := w.RecordUserTurn("implement the requested behavior", time.Now())
		item, err := w.AddItem("behavior", "the requested behavior works", engineering.OriginUser, []int{turn.Seq}, time.Now())
		if err != nil {
			return err
		}
		w.Items[0].Status = engineering.ItemVerified
		w.Items[0].Evidence = []engineering.EvidenceRef{{OperationID: "check-" + item.ID, Passed: true, Generation: st.Generation}}
		return nil
	})
	require.NoError(t, err)
	return a, sess.ID
}

func TestReviewFailureNeverBecomesPersistedDeliveryOrResetsTheContract(t *testing.T) {
	t.Parallel()
	a, id := verifiedWorkSession(t)
	// The answer was sent, but it is not a verified delivery of the task.
	require.NoError(t, recordDelivery(a.engineering, t.Context(), id, "answer"))
	e, _ := a.decideTurnCompletion(t.Context(), id, evaluatorDecision{Review: reviewFailed})
	require.NotEmpty(t, e.Unverified)
	st, err := a.engineering.Read(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, engineering.ItemVerified, st.Work.Items[0].Status)
	require.Equal(t, "incomplete", st.Completion.Verdict)

	// A new runtime reads the persisted obligation, including for goal claims.
	restarted := engineering.NewStore(filepath.Dir(a.engineering.Dir()))
	require.Contains(t, completionBlockers(t.Context(), restarted, a.sessions, id), "the independent review failed the work")
	_, err = recordWorkTurn(t.Context(), restarted, a.sessions, id, "also improve the error message", "")
	require.NoError(t, err)
	st, err = restarted.Read(t.Context(), id)
	require.NoError(t, err)
	require.Len(t, st.Work.Items, 1, "a later request must not erase the withheld contract")
	require.Equal(t, engineering.ItemVerified, st.Work.Items[0].Status)
}

func TestCompleteDecisionAndDeliveryAreCommittedTogether(t *testing.T) {
	t.Parallel()
	a, id := verifiedWorkSession(t)
	require.NoError(t, a.engineering.Update(t.Context(), id, func(st *engineering.State) error {
		st.Outcomes.Continuation.Count = maxSharedContinuations
		return nil
	}))
	e, _ := a.decideTurnCompletion(t.Context(), id, evaluatorDecision{Review: reviewPassed})
	require.Empty(t, e.Unverified)
	st, err := a.engineering.Read(t.Context(), id)
	require.NoError(t, err)
	require.True(t, st.Completion.CompleteAt(st))
	require.Equal(t, engineering.ItemDelivered, st.Work.Items[0].Status)
	require.Equal(t, "deliver", st.Work.Phase)
	restarted := engineering.NewStore(filepath.Dir(a.engineering.Dir()))
	completion, err := restarted.CompletionStatus(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "complete", completion.Verdict)
	_, err = recordWorkTurn(t.Context(), restarted, a.sessions, id, "start a different task", "")
	require.NoError(t, err)
	st, err = restarted.Read(t.Context(), id)
	require.NoError(t, err)
	require.Empty(t, st.Work.Items)
	require.Nil(t, st.Completion, "the old review is not applied to the new request")
	require.Zero(t, st.Outcomes.Continuation.Count, "a proven new human task receives its own continuation budget")
}

func TestUnifiedContinuationIsBoundedAndDoesNotRetryUnknownEffects(t *testing.T) {
	t.Parallel()
	a, id := verifiedWorkSession(t)
	for i := range maxSharedNoProgressContinuation + 2 {
		e, _ := a.decideTurnCompletion(t.Context(), id, evaluatorDecision{Review: reviewFailed})
		if i < maxSharedNoProgressContinuation {
			require.Contains(t, e.Continue, sharedContinuationMarker)
		} else {
			require.Empty(t, e.Continue, "unchanged evidence cannot trigger unlimited retries")
		}
	}
	a, id = verifiedWorkSession(t)
	e, _ := a.decideTurnCompletion(t.Context(), id, evaluatorDecision{Review: reviewUnavailable})
	require.Empty(t, e.Continue, "an unavailable reviewer is an explicit verification limit")
	require.NotEmpty(t, e.Unverified)
}

func TestReviewIsBoundToTheTaskAndWorkContract(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"task", "work"} {
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			a, id := verifiedWorkSession(t)
			_, _ = a.decideTurnCompletion(t.Context(), id, evaluatorDecision{Review: reviewPassed})
			if variant == "task" {
				sess, err := a.sessions.Get(t.Context(), id)
				require.NoError(t, err)
				sess.Todos = []session.Todo{{ID: "new", Content: "new acceptance criteria"}}
				_, err = a.sessions.Save(t.Context(), sess)
				require.NoError(t, err)
			} else {
				require.NoError(t, a.engineering.Update(t.Context(), id, func(st *engineering.State) error {
					st.Work.Items[0].Text = "changed acceptance criteria"
					return nil
				}))
			}
			in, ok := a.completionInput(t.Context(), id, evaluatorDecision{})
			require.True(t, ok)
			require.Equal(t, reviewUnavailable, in.Review, "an old review cannot certify different work at the same source generation")
		})
	}
}

func TestSavedCompletionIsStaleAfterSourceOrFindingChanges(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"source", "finding", "task-only"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			a, id := verifiedWorkSession(t)
			_, _ = a.decideTurnCompletion(t.Context(), id, evaluatorDecision{Review: reviewPassed})
			if change == "source" {
				require.NoError(t, os.WriteFile(filepath.Join(a.workingDir, "new.go"), []byte("package changed\n"), 0o644))
			} else {
				sess, err := a.sessions.Get(t.Context(), id)
				require.NoError(t, err)
				sess.Todos = []session.Todo{{ID: "task", Content: "repair", Status: session.TodoStatusCompleted}}
				_, err = a.sessions.Save(t.Context(), sess)
				require.NoError(t, err)
				if change == "finding" {
					openFindingFor(t, fakeEnv{sessions: a.sessions}, a.engineering, sess)
				}
			}
			completion, err := a.engineering.CompletionStatus(t.Context(), id)
			require.NoError(t, err)
			if change != "task-only" {
				require.Equal(t, "unverified", completion.Verdict)
			}
			_, err = recordWorkTurn(t.Context(), a.engineering, a.sessions, id, "another request", "")
			require.NoError(t, err)
			st, err := a.engineering.Read(t.Context(), id)
			require.NoError(t, err)
			require.NotEmpty(t, st.Work.Items)
		})
	}
}

func TestCompletionWithNoRequirementsStillChecksFindingsAndReadErrors(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"finding", "missing-session", "unreadable-state"} {
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			env := testEnv(t)
			sess, err := env.sessions.Create(t.Context(), "empty")
			require.NoError(t, err)
			a := newRootAgent(t.TempDir())
			a.sessions, a.engineering = env.sessions, engineering.NewStore(t.TempDir())
			switch variant {
			case "finding":
				// The helper needs a task identity only to write the review.
				sess.Todos = []session.Todo{{ID: "t", Content: "task"}}
				openFindingFor(t, env, a.engineering, sess)
			case "missing-session":
				sess.ID = "does-not-exist"
			case "unreadable-state":
				_, err = a.engineering.Read(t.Context(), sess.ID)
				require.NoError(t, err)
				path := filepath.Join(a.engineering.Dir(), engineering.Hash(sess.ID)+".json")
				require.NoError(t, os.Mkdir(path, 0o700))
			}
			e, _ := a.decideTurnCompletion(t.Context(), sess.ID, evaluatorDecision{Review: reviewPassed})
			require.NotEmpty(t, e.Unverified, "absence of requirements never bypasses unavailable evidence or findings")
			require.NotEmpty(t, completionBlockers(t.Context(), a.engineering, a.sessions, sess.ID))
		})
	}
}

func TestRegisteredDeliveryStageBlocksOtherwiseVerifiedCompletion(t *testing.T) {
	t.Parallel()
	a, id := verifiedWorkSession(t)
	require.NoError(t, a.engineering.Update(t.Context(), id, func(st *engineering.State) error {
		st.Delivery = &engineering.DeliveryPlan{
			Root: a.workingDir, Profile: "feature",
			Requirements:     []engineering.Requirement{{ID: "delivery", Description: "Ship the checked change", TaskIDs: []string{"task"}}},
			Stages:           []engineering.Stage{{ID: "release", Title: "Verify integration", TaskIDs: []string{"task"}}},
			TaskFingerprints: map[string]string{"task": "registered-spec"},
		}
		return nil
	}))
	e, _ := a.decideTurnCompletion(t.Context(), id, evaluatorDecision{Review: reviewPassed})
	require.NotEmpty(t, e.Unverified)
	require.Contains(t, e.Continue, "delivery stage release")
	st, err := a.engineering.Read(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, engineering.ItemVerified, st.Work.Items[0].Status)
	require.Equal(t, "incomplete", st.Completion.Verdict)
	require.NotEmpty(t, completionBlockers(t.Context(), a.engineering, a.sessions, id))
	require.NoError(t, a.engineering.Update(t.Context(), id, func(st *engineering.State) error {
		st.Delivery.CurrentStage = 1
		stage := &st.Delivery.Stages[0]
		stage.Passed, stage.SourceFingerprint = true, engineering.Hash("source checked by the stage adapter")
		stage.Checks = []engineering.Check{{Passed: true, Evidence: "observed stage result", RunID: "stage-check", TaskID: "stage:release"}}
		return nil
	}))
	e, _ = a.decideTurnCompletion(t.Context(), id, evaluatorDecision{Review: reviewPassed})
	require.Empty(t, e.Unverified, "a completed stage must clear its former gap")
	require.Empty(t, e.Continue)
}
