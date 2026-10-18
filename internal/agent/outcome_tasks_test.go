package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func sharedOutcomeAgent(t *testing.T, todos ...session.Todo) (*sessionAgent, string) {
	t.Helper()
	env := testEnv(t)
	current, err := env.sessions.Create(t.Context(), "shared outcomes")
	require.NoError(t, err)
	current.Todos = todos
	_, err = env.sessions.Save(t.Context(), current)
	require.NoError(t, err)
	return &sessionAgent{sessions: env.sessions, engineering: engineering.NewStore(t.TempDir()), maxStepsPerTurn: csync.NewValue(100)}, current.ID
}

func sharedEndTurn() *fantasy.AgentResult {
	return &fantasy.AgentResult{Steps: []fantasy.StepResult{{Response: fantasy.Response{
		FinishReason: fantasy.FinishReasonStop,
		Content: fantasy.ResponseContent{
			fantasy.ToolCallContent{ToolCallID: "task", ToolName: "todos", Input: `{}`},
			fantasy.ToolResultContent{ToolCallID: "task", ToolName: "todos", Result: fantasy.ToolResultOutputContentText{Text: "updated"}},
		},
	}}}}
}

func sharedVerifyTask(t *testing.T, a *sessionAgent, sessionID, taskID string) {
	t.Helper()
	id, err := a.engineering.Begin(t.Context(), sessionID, "verify-"+taskID, "verify", `{}`, taskID)
	require.NoError(t, err)
	sharedBindTaskFingerprint(t, a, sessionID, taskID, id)
	require.NoError(t, a.engineering.AppendVerificationCheck(t.Context(), sessionID, engineering.Check{
		TaskID: taskID, RunID: "verify-" + taskID, Tool: "test_run", Passed: true, Evidence: "Observed acceptance test passing",
	}))
	require.NoError(t, a.engineering.FinishObserved(t.Context(), sessionID, id, true, false, engineering.Hash("observed result"), true))
}

func sharedBindTaskFingerprint(t *testing.T, a *sessionAgent, sessionID, taskID, opID string) {
	t.Helper()
	current, err := a.sessions.Get(t.Context(), sessionID)
	require.NoError(t, err)
	for _, todo := range current.Todos {
		if todo.ID != taskID {
			continue
		}
		require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error {
			for i := range st.Operations {
				if st.Operations[i].ID == opID {
					st.Operations[i].TaskFingerprint = session.TaskFingerprint(todo)
				}
			}
			return nil
		}))
		return
	}
}

func TestSharedTaskRequirementsRejectModelAndUnrelatedProof(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t,
		session.Todo{ID: "first", Content: "Build first", Status: session.TodoStatusCompleted, Verification: "passed", Evidence: []session.TodoEvidence{{Kind: "command", Detail: "The model says tests passed"}}},
		session.Todo{ID: "second", Content: "Build second", Status: session.TodoStatusPending, DependsOn: []string{"first"}},
	)
	sharedVerifyTask(t, a, sessionID, "unrelated")
	requirements, err := sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseApplied, requirements[0].Phase)
	require.Equal(t, outcome.StateWaiting, outcome.Resolve(requirements)[1].State)
	sharedVerifyTask(t, a, sessionID, "first")
	requirements, err = sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseVerified, requirements[0].Phase)
	require.Equal(t, outcome.StateReady, outcome.Resolve(requirements)[1].State)
	state, err := a.engineering.Read(t.Context(), sessionID)
	require.NoError(t, err)
	require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error {
		st.Outcomes.Record(outcome.Record{OpID: "later-edit", Tool: "edit", Phase: outcome.PhaseApplied, Mutation: true, At: state.Operations[len(state.Operations)-1].FinishedAt + 1})
		return nil
	}))
	requirements, err = sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseApplied, requirements[0].Phase, "later mutations require fresh checks")
}

func TestWorkContractJoinsSharedCompletion(t *testing.T) {
	t.Parallel()
	a, id := sharedOutcomeAgent(t)
	_, err := a.engineering.UpdateWork(t.Context(), id, func(w *engineering.WorkState, _ engineering.State) error {
		w.RecordUserTurn("fix export", time.Now())
		_, err := w.AddItem("behavior", "export works", engineering.OriginUser, []int{1}, time.Now())
		return err
	})
	require.NoError(t, err)
	decision := a.sharedCompletionPrompt(t.Context(), id, sharedEndTurn(), 0)
	require.True(t, decision.Continue)
	require.Contains(t, decision.Prompt, "work:R1")
	_, err = a.engineering.UpdateWork(t.Context(), id, func(w *engineering.WorkState, st engineering.State) error {
		w.Items[0].Status = engineering.ItemVerified
		w.Items[0].Evidence = []engineering.EvidenceRef{{Passed: true, Generation: st.Generation}}
		return nil
	})
	require.NoError(t, err)
	decision = a.sharedCompletionPrompt(t.Context(), id, sharedEndTurn(), 0)
	require.False(t, decision.Continue)
	require.Zero(t, decision.Open)
	require.NoError(t, a.engineering.Update(t.Context(), id, func(st *engineering.State) error { st.Generation++; return nil }))
	decision = a.sharedCompletionPrompt(t.Context(), id, sharedEndTurn(), 0)
	require.True(t, decision.Continue, "stale verification cannot end the contract")
}

func TestSharedTaskRequirementsRequireActualOperationForOutputEvidence(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Finish task", Status: session.TodoStatusCompleted})
	require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error {
		st.Outcomes.Add(outcome.Evidence{ID: "proof", Kind: outcome.KindOutput, Source: "invented", Subject: "task", Claim: "test_passed"})
		return nil
	}))
	requirements, err := sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseApplied, requirements[0].Phase)
	id, err := a.engineering.Begin(t.Context(), sessionID, "tests", "test_run", `{}`, "task")
	require.NoError(t, err)
	sharedBindTaskFingerprint(t, a, sessionID, "task", id)
	require.NoError(t, a.engineering.FinishObserved(t.Context(), sessionID, id, true, false, engineering.Hash("passing tests"), true))
	require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error {
		st.Outcomes.Add(outcome.Evidence{ID: "actual", Kind: outcome.KindOutput, Source: id, Subject: "task", Claim: "test_passed"})
		return nil
	}))
	requirements, err = sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseVerified, requirements[0].Phase)
}

func TestSharedCompletionContinuesPendingAndUnverifiedWork(t *testing.T) {
	t.Parallel()
	for _, status := range []session.TodoStatus{session.TodoStatusPending, session.TodoStatusCompleted} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Finish authorized task", Status: status})
			decision := a.sharedCompletionPrompt(t.Context(), sessionID, sharedEndTurn(), 0)
			require.True(t, decision.Continue)
			require.Equal(t, 1, decision.Open)
			require.Contains(t, decision.Prompt, sharedContinuationMarker)
			require.Contains(t, decision.Prompt, "task-scoped machine checks")
		})
	}
}

func TestSharedCompletionDoesNotOverrideTurnStops(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"queued", "canceled", "stop_turn", "length", "unrelated", "failed_tool", "step_limit", "unknown", "blocked", "subagent"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Finish task", Status: session.TodoStatusPending})
			ctx := t.Context()
			result, queued := sharedEndTurn(), 0
			switch reason {
			case "queued":
				queued = 1
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "stop_turn":
				result.Steps[0].Content = append(result.Steps[0].Content, fantasy.ToolResultContent{ToolName: "ask_user", StopTurn: true})
			case "length":
				result.Steps[0].FinishReason = fantasy.FinishReasonLength
			case "unrelated":
				result.Steps[0].Content = fantasy.ResponseContent{fantasy.TextContent{Text: "An unrelated answer"}}
			case "failed_tool":
				result.Steps[0].Content = fantasy.ResponseContent{fantasy.ToolResultContent{ToolName: "todos", Result: fantasy.ToolResultOutputContentError{Error: context.DeadlineExceeded}}}
			case "step_limit":
				a.maxStepsPerTurn.Set(1)
			case "unknown", "blocked":
				require.NoError(t, a.engineering.Update(ctx, sessionID, func(st *engineering.State) error {
					if reason == "unknown" {
						st.Outcomes.Record(outcome.Record{OpID: "unknown", Tool: "mcp_write", Phase: outcome.PhaseUnknown, Mutation: true})
					} else {
						st.Paused = true
					}
					return nil
				}))
			case "subagent":
				a.isSubAgent = true
			}
			decision := a.sharedCompletionPrompt(ctx, sessionID, result, queued)
			require.False(t, decision.Continue)
			state, err := a.engineering.Read(t.Context(), sessionID)
			require.NoError(t, err)
			require.Zero(t, state.Outcomes.Continuation.Count)
		})
	}
}

func TestSharedCompletionBoundsSurviveRestart(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Finish task", Status: session.TodoStatusPending})
	require.True(t, a.sharedCompletionPrompt(t.Context(), sessionID, sharedEndTurn(), 0).Continue)
	require.True(t, a.sharedCompletionPrompt(t.Context(), sessionID, sharedEndTurn(), 0).Continue)
	// Reusing only the durable store and session service simulates restart;
	// a new agent cannot clear the existing no-progress bound.
	restarted := &sessionAgent{sessions: a.sessions, engineering: engineering.NewStore(filepath.Dir(a.engineering.Dir())), maxStepsPerTurn: csync.NewValue(100)}
	decision := restarted.sharedCompletionPrompt(t.Context(), sessionID, sharedEndTurn(), 0)
	require.False(t, decision.Continue)
	require.Equal(t, "no_progress", decision.StopReason)
	state, err := a.engineering.Read(t.Context(), sessionID)
	require.NoError(t, err)
	require.Equal(t, "no_progress", state.Outcomes.Continuation.StopReason)
}

func TestSharedCompletionTotalLimitWithProgress(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Finish task", Status: session.TodoStatusPending})
	for range maxSharedContinuations {
		require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error { st.Generation++; return nil }))
		require.True(t, a.sharedCompletionPrompt(t.Context(), sessionID, sharedEndTurn(), 0).Continue)
	}
	decision := a.sharedCompletionPrompt(t.Context(), sessionID, sharedEndTurn(), 0)
	require.False(t, decision.Continue)
	require.Equal(t, "continuation_limit", decision.StopReason)
}

func TestSharedCompletionTaskRewriteDoesNotResetBounds(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Finish task", Status: session.TodoStatusPending})
	for range maxSharedNoProgressContinuation {
		require.True(t, a.sharedCompletionPrompt(t.Context(), sessionID, sharedEndTurn(), 0).Continue)
	}
	current, err := a.sessions.Get(t.Context(), sessionID)
	require.NoError(t, err)
	current.Todos[0].Content = "Rewrite the same unfinished task"
	_, err = a.sessions.Save(t.Context(), current)
	require.NoError(t, err)
	decision := a.sharedCompletionPrompt(t.Context(), sessionID, sharedEndTurn(), 0)
	require.False(t, decision.Continue)
	require.Equal(t, "no_progress", decision.StopReason)
}

func TestSharedTaskDispatchRequiresVerifiedUpstream(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t,
		session.Todo{ID: "upstream", Content: "Produce the implementation", Status: session.TodoStatusCompleted, Verification: "passed"},
		session.Todo{ID: "downstream", Content: "Use the implementation", Status: session.TodoStatusPending, DependsOn: []string{"upstream"}},
	)
	refusal := sharedTaskDispatchRefusal(t.Context(), a.engineering, a.sessions, sessionID, "downstream")
	require.Contains(t, refusal, "waiting_on_dependency")
	require.Contains(t, refusal, "upstream is not verified")
	require.Empty(t, sharedTaskDispatchRefusal(t.Context(), a.engineering, a.sessions, sessionID, "specialist-work"), "non-todo specialist work keeps its existing admission path")
	sharedVerifyTask(t, a, sessionID, "upstream")
	require.Empty(t, sharedTaskDispatchRefusal(t.Context(), a.engineering, a.sessions, sessionID, "downstream"))
}

func TestSharedTaskDispatchBlocksFailedAndUnknownUpstream(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"failed", "unknown"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			upstream := session.Todo{ID: "upstream", Content: "Produce the implementation", Status: session.TodoStatusPending}
			if reason == "failed" {
				upstream.Verification = "failed"
			}
			a, sessionID := sharedOutcomeAgent(t, upstream, session.Todo{ID: "downstream", Content: "Use the implementation", Status: session.TodoStatusPending, DependsOn: []string{"upstream"}})
			if reason == "unknown" {
				require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error {
					st.Outcomes.Record(outcome.Record{OpID: "opaque", Tool: "mcp_write", Phase: outcome.PhaseUnknown, Mutation: true})
					return nil
				}))
			}
			refusal := sharedTaskDispatchRefusal(t.Context(), a.engineering, a.sessions, sessionID, "downstream")
			if reason == "failed" {
				require.Contains(t, refusal, "blocked_by_dependency")
			} else {
				require.Contains(t, refusal, "unknown")
				require.Contains(t, refusal, "do not replay")
			}
			require.Empty(t, sharedTaskDispatchRefusal(t.Context(), a.engineering, a.sessions, sessionID, "non-todo"))
		})
	}
}

func TestSharedTaskProofDoesNotSurviveSpecificationChange(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"content", "acceptance"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			a, sessionID := sharedOutcomeAgent(t,
				session.Todo{ID: "upstream", Content: "Implement original behavior", Status: session.TodoStatusCompleted, Verification: "passed", AcceptanceCriteria: []string{"Return the original value"}, Evidence: []session.TodoEvidence{{Kind: "command", Detail: "Actual machine check is retained separately"}}},
				session.Todo{ID: "downstream", Content: "Use upstream result", Status: session.TodoStatusPending, DependsOn: []string{"upstream"}},
			)
			sharedVerifyTask(t, a, sessionID, "upstream")
			require.Empty(t, sharedTaskDispatchRefusal(t.Context(), a.engineering, a.sessions, sessionID, "downstream"))
			current, err := a.sessions.Get(t.Context(), sessionID)
			require.NoError(t, err)
			if change == "content" {
				current.Todos[0].Content = "Implement a different behavior under the same ID"
			} else {
				current.Todos[0].AcceptanceCriteria = []string{"Return a different value"}
			}
			_, err = a.sessions.Save(t.Context(), current)
			require.NoError(t, err)
			requirements, err := sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
			require.NoError(t, err)
			require.Equal(t, outcome.PhaseApplied, requirements[0].Phase)
			require.Contains(t, sharedTaskDispatchRefusal(t.Context(), a.engineering, a.sessions, sessionID, "downstream"), "waiting_on_dependency")
		})
	}
}

func TestSharedTaskProofRemainsStaleAfterMutationRecordEviction(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Finish task", Status: session.TodoStatusCompleted})
	sharedVerifyTask(t, a, sessionID, "task")
	state, err := a.engineering.Read(t.Context(), sessionID)
	require.NoError(t, err)
	mutationAt := state.Operations[0].FinishedAt + 1
	require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error {
		st.Outcomes.Record(outcome.Record{OpID: "evicted-mutation", Tool: "edit", Phase: outcome.PhaseApplied, Mutation: true, At: mutationAt})
		for i := range 128 {
			st.Outcomes.Record(outcome.Record{OpID: fmt.Sprintf("later-read-%d", i), Tool: "view", Phase: outcome.PhaseVerified, At: mutationAt + int64(i+1)})
		}
		return nil
	}))
	state, err = a.engineering.Read(t.Context(), sessionID)
	require.NoError(t, err)
	for _, record := range state.Outcomes.Records {
		require.NotEqual(t, "evicted-mutation", record.OpID)
	}
	require.Equal(t, mutationAt, state.Outcomes.LastMutationAt)
	a.engineering = engineering.NewStore(filepath.Dir(a.engineering.Dir()))
	requirements, err := sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseApplied, requirements[0].Phase, "evicting the mutation record cannot renew an earlier proof")
}

func TestSharedLegacyProofRequiresCurrentRoleBinding(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Finish task", Status: session.TodoStatusCompleted})
	check := engineering.Check{TaskID: "task", RunID: "legacy-check", Tool: "test_run", Passed: true, Evidence: "Observed passing task test"}
	opID, err := a.engineering.Begin(t.Context(), sessionID, check.RunID, "verify", `{}`, "task")
	require.NoError(t, err)
	require.NoError(t, a.engineering.AppendVerificationCheck(t.Context(), sessionID, check))
	require.NoError(t, a.engineering.FinishObserved(t.Context(), sessionID, opID, true, false, engineering.Hash("legacy machine result"), true))
	requirements, err := sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseApplied, requirements[0].Phase, "ordinary tasks cannot reuse a proof without a bound specification")
	current, err := a.sessions.Get(t.Context(), sessionID)
	require.NoError(t, err)
	require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error {
		st.RoleExecutions["task"] = engineering.RoleExecution{TaskID: "task", TaskFingerprint: session.TaskFingerprint(current.Todos[0]), MachineChecks: []engineering.Check{check}}
		return nil
	}))
	requirements, err = sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseVerified, requirements[0].Phase, "the same legacy check may be bound by the current role specification")
}

func TestSharedTaskProofFailsClosedForEqualMutationTimestamp(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Finish task", Status: session.TodoStatusCompleted})
	sharedVerifyTask(t, a, sessionID, "task")
	state, err := a.engineering.Read(t.Context(), sessionID)
	require.NoError(t, err)
	require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error {
		st.Outcomes.Record(outcome.Record{OpID: "same-tick-mutation", Tool: "edit", Phase: outcome.PhaseApplied, Mutation: true, At: state.Operations[0].FinishedAt})
		return nil
	}))
	requirements, err := sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseApplied, requirements[0].Phase, "millisecond equality cannot establish that verification happened after the mutation")
}

func desktopEndTurn() *fantasy.AgentResult {
	return &fantasy.AgentResult{Steps: []fantasy.StepResult{{Response: fantasy.Response{
		FinishReason: fantasy.FinishReasonStop,
		Content: fantasy.ResponseContent{
			fantasy.ToolCallContent{ToolCallID: "c", ToolName: "computer", Input: `{"action":"key","key":"enter"}`},
			fantasy.ToolResultContent{ToolCallID: "c", ToolName: "computer", Result: fantasy.ToolResultOutputContentText{Text: "Pressed enter."}},
			fantasy.TextContent{Text: "Tüm adımlar tamamlandı ve doğrulandı."},
		},
	}}}}
}

// A desktop turn without a task plan used to bypass every completion check:
// the model's "all verified" text ended the turn even with an unknown input
// effect on record. Now runtime-observed open work stops it honestly, while
// a clean desktop turn costs no extra turn and stale todos stay untouched.
func TestDesktopTurnWithoutPlanAnswersForObservedOpenWork(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "old", Content: "Earlier unverifiable item", Status: session.TodoStatusCompleted})
	decision := a.sharedCompletionPrompt(t.Context(), sessionID, desktopEndTurn(), 0)
	require.False(t, decision.Continue, "a clean desktop turn needs no plan or extra turn")
	require.Empty(t, decision.StopReason)
	require.Zero(t, decision.Open)

	require.NoError(t, a.engineering.Update(t.Context(), sessionID, func(st *engineering.State) error {
		st.Outcomes.Record(outcome.Record{OpID: "enter", Tool: "computer", Phase: outcome.PhaseUnknown, Mutation: true, Subjects: []string{"desktop"}})
		return nil
	}))
	decision = a.sharedCompletionPrompt(t.Context(), sessionID, desktopEndTurn(), 0)
	require.False(t, decision.Continue, "an unknown effect is reconciled, never replayed by a continuation")
	require.Equal(t, "unresolved_or_blocked", decision.StopReason)
	require.Equal(t, 1, decision.Open, "only runtime-observed work counts, not the stale todo")
}

// A window that appeared after an input but was never reported becomes one
// actionable requirement for the completion check.
func TestDesktopTurnRequirementsIncludeUnobservedWindows(t *testing.T) {
	t.Parallel()
	a, sessionID := sharedOutcomeAgent(t, session.Todo{ID: "old", Content: "Earlier item", Status: session.TodoStatusPending})
	all, err := sharedTaskRequirements(t.Context(), a.engineering, a.sessions, sessionID)
	require.NoError(t, err)
	got, err := desktopTurnRequirements(t.Context(), a.engineering, sessionID, all, []string{`the earlier key on window 11 opened "sonuc - Not Defteri" (window_id 526168, notepad.exe), which was never observed`})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, outcome.PhaseApplied, got[0].Phase)
	require.Contains(t, got[0].Description, "526168")
	resolved := outcome.Resolve(got)
	require.Equal(t, outcome.StateReady, resolved[0].State)
}

type completionDesktopBackend struct {
	computer.Backend
	opened bool
}

func (b *completionDesktopBackend) ScreenSize() (computer.Size, error) {
	return computer.Size{Width: 100, Height: 100}, nil
}
func (b *completionDesktopBackend) ForegroundWindow() string { return "11" }
func (b *completionDesktopBackend) KeyPress(string) error    { return nil }
func (b *completionDesktopBackend) ListWindows(context.Context) (json.RawMessage, error) {
	if b.opened {
		return json.RawMessage(`[{"window_id":"11","name":"Source","foreground":false},{"window_id":"22","name":"Opened document","foreground":true}]`), nil
	}
	return json.RawMessage(`[{"window_id":"11","name":"Source","foreground":true}]`), nil
}

type completionDesktopPermission struct{ permission.Service }

func (completionDesktopPermission) Request(context.Context, permission.CreatePermissionRequest) (bool, error) {
	return true, nil
}

func TestPlannedDesktopCompletionIncludesUnreportedTransition(t *testing.T) {
	a, id := sharedOutcomeAgent(t, session.Todo{ID: "task", Content: "Open document and verify", Status: session.TodoStatusCompleted})
	sharedVerifyTask(t, a, id, "task")
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, id)
	backend := &completionDesktopBackend{}
	disabled := false
	tool := tools.NewComputerTool(completionDesktopPermission{}, t.TempDir(), config.ToolComputer{Overlay: &disabled}, backend, func() bool { return true })
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "open", Input: `{"action":"key","key":"enter","automation":{"window_id":"11"}}`})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	backend.opened = true
	require.NotEmpty(t, tools.DesktopOpenTransitions(ctx, id))
	result := desktopEndTurn()
	result.Steps[0].Content = append(result.Steps[0].Content, fantasy.ToolResultContent{ToolCallID: "plan", ToolName: "todos", Result: fantasy.ToolResultOutputContentText{Text: "updated"}})
	got := a.sharedCompletionPrompt(ctx, id, result, 0)
	require.True(t, got.Continue)
	require.Equal(t, 1, got.Open)
	require.Contains(t, got.Prompt, "22")
}
