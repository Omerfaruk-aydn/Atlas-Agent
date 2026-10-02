package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

type interruptedTodoMutation struct {
	session.Service
	AfterSave bool
}

func (s interruptedTodoMutation) CompareAndSwapTodos(ctx context.Context, id, expected string, todos []session.Todo) (session.Session, error) {
	if s.AfterSave {
		if _, err := s.Service.CompareAndSwapTodos(ctx, id, expected, todos); err != nil {
			return session.Session{}, err
		}
	}
	return session.Session{}, context.Canceled
}

func TestFindingsRemediationReconcilesWithoutDuplicateTasks(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("broken"), 0o600))
	sess, err := env.sessions.Create(t.Context(), "remediate")
	require.NoError(t, err)
	task := session.Todo{ID: "api", Content: "Implement", Agent: "backend", Status: session.TodoStatusInProgress, OwnedPaths: []string{"api.go"}, AcceptanceCriteria: []string{"Returns value"}}
	sess.Todos = []session.Todo{task}
	_, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	s := engineering.NewStore(t.TempDir())
	c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(root), sessions: env.sessions, permissions: env.permissions, engineering: s}
	env.permissions.SetMode(permission.ModeBypass)
	fp, err := engineering.SourceFingerprint(t.Context(), root, s.Dir())
	require.NoError(t, err)
	f := engineering.Finding{Root: root, TaskID: task.ID, TaskFingerprint: session.TaskFingerprint(task), ReviewerExecutionID: "review", ImplementerExecutionID: "impl", SourceFingerprint: fp, Status: "open", Path: "api.go", StartLine: 1, EndLine: 1, Severity: 1, Issue: "Empty result", Expected: "Return value", Evidence: "Observed", Checks: []engineering.ContractCheck{{Name: "check", Tool: "bash", InputJSON: `{"command":"true"}`}}}
	f.ID = engineering.FindingID(f)
	plan := &engineering.DeliveryPlan{Root: root, Profile: "feature", Requirements: []engineering.Requirement{{ID: "user", Description: "API", TaskIDs: []string{task.ID}}}, Stages: []engineering.Stage{{ID: "api", Title: "API", TaskIDs: []string{task.ID}}}, TaskFingerprints: map[string]string{task.ID: session.TaskFingerprint(task)}}
	require.NoError(t, s.Update(t.Context(), sess.ID, func(st *engineering.State) error {
		st.Delivery = plan
		st.Operations = []engineering.Operation{{ID: "review", Tool: "agent", AgentName: "review", TaskID: task.ID, Status: "completed"}, {ID: "impl", Tool: "agent", AgentName: "backend", TaskID: task.ID, Status: "completed"}}
		return nil
	}))
	_, err = s.SaveFinding(t.Context(), sess.ID, f, 0)
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	// Reconcile both sides of the task-save boundary without creating duplicates.
	for _, afterSave := range []bool{false, true} {
		c.sessions = interruptedTodoMutation{Service: env.sessions, AfterSave: afterSave}
		interrupted, err := c.remediationWorkflow(ctx, WorkflowParams{Action: "remediate", FindingIDs: []string{f.ID}}, fantasy.ToolCall{ID: "interrupted"}, nil)
		require.NoError(t, err)
		require.True(t, interrupted.IsError)
	}
	c.sessions = env.sessions
	for _, id := range []string{"first", "retry"} {
		resp, err := c.remediationWorkflow(ctx, WorkflowParams{Action: "remediate", FindingIDs: []string{f.ID}}, fantasy.ToolCall{ID: id}, nil)
		require.NoError(t, err)
		require.False(t, resp.IsError, resp.Content)
	}
	latest, err := env.sessions.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, latest.Todos, 2)
	require.Equal(t, f.RemediationID(), latest.Todos[1].ID)
	require.Contains(t, latest.Todos[0].DependsOn, f.RemediationID())
	st, err := s.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, st.Delivery.Stages, 2)
	require.Equal(t, f.RemediationID(), st.Delivery.Stages[0].TaskIDs[0])
	resp, err := c.remediationWorkflow(ctx, WorkflowParams{Action: "finding", FindingAction: "waive", FindingIDs: []string{f.ID}, Evidence: "Approved"}, fantasy.ToolCall{ID: "forged-waiver"}, nil)
	require.NoError(t, err)
	require.True(t, resp.IsError)
}

func TestFindingsOverlappingRepairsAreSerialized(t *testing.T) {
	t.Parallel()
	parent := session.Todo{ID: "api", Content: "Implement", Agent: "backend", Status: session.TodoStatusInProgress, OwnedPaths: []string{"api.go"}, AcceptanceCriteria: []string{"Works"}}
	first := engineering.Finding{ID: engineering.Hash("first"), TaskID: parent.ID, Path: "api.go", Issue: "First defect", Expected: "First fixed"}
	second := first
	second.ID, second.Issue = engineering.Hash("second"), "Second defect"
	todos, _, err := compileRemediation([]session.Todo{parent}, nil, []engineering.Finding{first, second})
	require.NoError(t, err)
	require.Len(t, todos, 3)
	require.Contains(t, todos[2].DependsOn, first.RemediationID())
	wave, err := session.ReadyTaskWave(todos, 4)
	require.NoError(t, err)
	require.Len(t, wave, 1)
	require.Equal(t, first.RemediationID(), wave[0].ID)
}

func TestFindingsReviewImportsRuntimeIdentities(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("broken"), 0o600))
	sess, err := env.sessions.Create(t.Context(), "finding-review")
	require.NoError(t, err)
	task := session.Todo{ID: "api", Content: "Implement", Agent: "backend", Status: session.TodoStatusInProgress, OwnedPaths: []string{"api.go"}, AcceptanceCriteria: []string{"Returns value"}}
	sess.Todos = []session.Todo{task}
	_, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	s := engineering.NewStore(t.TempDir())
	c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(root), sessions: env.sessions, engineering: s}
	handoff := subagents.Handoff{TaskID: task.ID, Summary: "Implemented", ChangedFiles: []string{"api.go"}, Checks: []subagents.HandoffCheck{}, Risks: []string{}, Dependencies: []string{}, Decision: "ready"}
	require.NoError(t, s.Update(t.Context(), sess.ID, func(st *engineering.State) error {
		st.Operations = []engineering.Operation{{ID: "impl", Tool: "agent", AgentName: "backend", TaskID: task.ID, Status: "completed"}}
		st.RoleExecutions[task.ID] = engineering.RoleExecution{ExecutionID: "impl", TaskID: task.ID, TaskFingerprint: session.TaskFingerprint(task), Agent: "backend", Handoff: &handoff, RequireReview: true, Root: root}
		return nil
	}))
	specialist := fantasy.NewAgentTool("agent", "fixture", func(ctx context.Context, p AgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		result := handoff
		result.ChangedFiles = []string{}
		result.Decision = "passed"
		if p.AgentName == "review" {
			result.Decision = "changes_required"
			result.Findings = []subagents.HandoffFinding{{Path: "api.go", StartLine: 1, EndLine: 1, Severity: 1, Issue: "Empty result", Expected: "Return value", Evidence: "Inspected"}}
		}
		return fantasy.NewTextResponse(workflowJSON(result)), nil
	})
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return (&guardedTool{AgentTool: specialist, store: s}).Run(ctx, call)
	}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	resp, err := c.reviewTask(ctx, WorkflowParams{TaskID: task.ID, Checks: []tools.VerificationStep{{Name: "check", Tool: "bash", Input: []byte(`{"command":"true"}`)}}}, fantasy.ToolCall{ID: "quality"}, sess, invoke)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	findings, err := s.TaskFindings(ctx, sess.ID, task.ID)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, "impl", findings[0].ImplementerExecutionID)
	require.NotEmpty(t, findings[0].ReviewerExecutionID)
	require.NotEqual(t, findings[0].ImplementerExecutionID, findings[0].ReviewerExecutionID)
}

func TestFindingsVerifyRequiresActualReviewAndChecks(t *testing.T) {
	for _, mode := range []string{"passed", "forged-check", "source-change"} {
		t.Run(mode, func(t *testing.T) {
			env := testEnv(t)
			root := t.TempDir()
			_, err := engineering.Git(t.Context(), root, "init")
			require.NoError(t, err)
			file := filepath.Join(root, "api.go")
			require.NoError(t, os.WriteFile(file, []byte("broken"), 0o600))
			sess, err := env.sessions.Create(t.Context(), "finding-verification")
			require.NoError(t, err)
			task := session.Todo{ID: "api", Content: "Implement", Agent: "backend", Status: session.TodoStatusInProgress, OwnedPaths: []string{"api.go"}, AcceptanceCriteria: []string{"Returns value"}}
			s := engineering.NewStore(t.TempDir())
			fp, err := engineering.SourceFingerprint(t.Context(), root, s.Dir())
			require.NoError(t, err)
			f := engineering.Finding{Root: root, TaskID: task.ID, TaskFingerprint: session.TaskFingerprint(task), ReviewerExecutionID: "review", ImplementerExecutionID: "impl", SourceFingerprint: fp, Status: "open", Path: "api.go", StartLine: 1, EndLine: 1, Severity: 1, Issue: "Empty result", Expected: "Return value", Evidence: "Observed", Checks: []engineering.ContractCheck{{Name: "check", Tool: "bash", InputJSON: `{"command":"true"}`}}}
			f.ID = engineering.FindingID(f)
			sess.Todos = []session.Todo{task, {ID: f.RemediationID(), Content: "Repair finding", Agent: "debug", Status: session.TodoStatusPending, OwnedPaths: []string{"api.go"}}}
			_, err = env.sessions.Save(t.Context(), sess)
			require.NoError(t, err)
			require.NoError(t, s.Update(t.Context(), sess.ID, func(st *engineering.State) error {
				st.Operations = []engineering.Operation{{ID: "review", Tool: "agent", AgentName: "review", TaskID: task.ID, Status: "completed"}, {ID: "impl", Tool: "agent", AgentName: "backend", TaskID: task.ID, Status: "completed"}}
				return nil
			}))
			ref, err := s.SaveFinding(t.Context(), sess.ID, f, 0)
			require.NoError(t, err)
			f.Status, f.RemediationTaskID = "fixed", f.RemediationID()
			_, err = s.SaveFinding(t.Context(), sess.ID, f, ref.Revision)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(file, []byte("fixed"), 0o600))
			c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(root), sessions: env.sessions, engineering: s}
			ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
			next := sess
			next.Todos = append([]session.Todo{}, sess.Todos...)
			next.Todos[0].Status, next.Todos[0].Verification = session.TodoStatusCompleted, "passed"
			next.Todos[0].Evidence = []session.TodoEvidence{{Kind: "command", Detail: "Reported checks"}}
			require.ErrorContains(t, session.EngineeringCompletionGate(s)(ctx, sess, next), "finding gate")
			specialist := fantasy.NewAgentTool("agent", "fixture", func(ctx context.Context, p AgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				require.Equal(t, "review", p.AgentName)
				require.True(t, p.QualityOnly)
				if mode == "source-change" {
					require.NoError(t, os.WriteFile(file, []byte("changed during review"), 0o600))
				}
				h := subagents.Handoff{TaskID: task.ID, Summary: "Reinspected actual source", Decision: "passed", ChangedFiles: []string{}, Checks: []subagents.HandoffCheck{}, Risks: []string{}, Dependencies: []string{}}
				return fantasy.NewTextResponse(workflowJSON(h)), nil
			})
			bash := fantasy.NewAgentTool("bash", "fixture", func(ctx context.Context, p tools.BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				zero := 0
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Observed"), tools.BashResponseMetadata{ExitCode: &zero, Output: "Observed"}), nil
			})
			var invoke tools.ToolInvoker
			verify := tools.NewVerifyTool(root, s, func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				return invoke(ctx, call)
			})
			invoke = func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var tool fantasy.AgentTool
				switch call.Name {
				case "agent":
					tool = specialist
				case "bash":
					tool = bash
				case "verify":
					if mode == "forged-check" {
						return fantasy.NewTextResponse(`{"passed":true}`), nil
					}
					tool = verify
				}
				return (&guardedTool{AgentTool: tool, store: s}).Run(ctx, call)
			}
			resp, err := c.remediationWorkflow(ctx, WorkflowParams{Action: "finding", FindingAction: "verify", FindingIDs: []string{f.ID}}, fantasy.ToolCall{ID: "check"}, invoke)
			require.NoError(t, err)
			require.Equal(t, mode != "passed", resp.IsError, resp.Content)
			latest, _, err := s.ReadFinding(ctx, sess.ID, f.ID)
			require.NoError(t, err)
			require.Equal(t, mode == "passed", latest.Status == "verified")
			gateErr := session.EngineeringCompletionGate(s)(ctx, sess, next)
			if mode == "passed" {
				require.NoError(t, gateErr)
			} else {
				require.Error(t, gateErr)
			}
		})
	}
}
