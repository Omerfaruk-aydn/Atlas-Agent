package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPlatformCheckChild(t *testing.T) {
	if os.Getenv("ATLAS_PLATFORM_CHECK_CHILD") != "1" {
		return
	}
	data, err := os.ReadFile("api.txt")
	if err != nil || string(data) != "fixed" {
		fmt.Println("Observed invalid API result")
		os.Exit(7)
	}
	fmt.Println("Observed fixed API result")
	os.Exit(0)
}

func TestPlatformIntegrationRecoveryAndRemediation(t *testing.T) {
	env := testEnv(t)
	root, dataDir := t.TempDir(), t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	source := filepath.Join(root, "api.txt")
	require.NoError(t, os.WriteFile(source, []byte("broken"), 0o600))
	sess, err := env.sessions.Create(t.Context(), "platform")
	require.NoError(t, err)
	sess.Todos = []session.Todo{
		{ID: "api", Content: "Provide stable API", Agent: "backend", Status: session.TodoStatusInProgress, OwnedPaths: []string{"api.txt"}, AcceptanceCriteria: []string{"Real check passes"}},
		{ID: "consumer", Content: "Consume shared API", Agent: "frontend", Status: session.TodoStatusInProgress, OwnedPaths: []string{"client.txt"}, AcceptanceCriteria: []string{"Shared contract passes"}},
	}
	sess, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	store := engineering.NewStore(dataDir)
	c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(root), sessions: env.sessions, permissions: env.permissions, engineering: store}
	env.permissions.SetMode(permission.ModeBypass)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	exe, err := os.Executable()
	require.NoError(t, err)
	checkInput := json.RawMessage(`{"command":"fixture-check"}`)
	checks := []tools.VerificationStep{{Name: "api-check", Tool: "bash", Input: checkInput}}
	var invoke tools.ToolInvoker
	bash := fantasy.NewAgentTool("bash", "Actual child-process check", func(ctx context.Context, _ tools.BashParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestPlatformCheckChild$")
		cmd.Dir, cmd.Env = root, append(os.Environ(), "ATLAS_PLATFORM_CHECK_CHILD=1")
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				return fantasy.ToolResponse{}, err
			}
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(string(out)), tools.BashResponseMetadata{ExitCode: &code, Output: string(out)}), nil
	})
	specialist := fantasy.NewAgentTool("agent", "Mock specialist; no provider request", func(ctx context.Context, p AgentParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
		id := engineering.GetScope(ctx, sess.ID).TaskID
		decision := "passed"
		if p.AgentName == "debug" {
			require.NoError(t, os.WriteFile(source, []byte("fixed"), 0o600))
			require.NoError(t, store.Update(ctx, sess.ID, func(st *engineering.State) error { st.Generation++; return nil }))
			decision = "ready"
		}
		return fantasy.NewTextResponse(workflowJSON(subagents.Handoff{TaskID: id, Summary: "Inspected fixture", Decision: decision, ChangedFiles: []string{}, Checks: []subagents.HandoffCheck{}, Risks: []string{}, Dependencies: []string{}})), nil
	})
	verify := tools.NewVerifyTool(root, store, func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return invoke(ctx, call)
	})
	invoke = func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var target fantasy.AgentTool
		switch call.Name {
		case "bash":
			target = bash
		case "verify":
			target = verify
		case "agent":
			target = specialist
		default:
			t.Fatalf("Unexpected tool %s", call.Name)
		}
		return (&guardedTool{AgentTool: target, store: store}).Run(ctx, call)
	}
	sources, err := engineering.CaptureSources(ctx, root, []string{"api.txt"})
	require.NoError(t, err)
	contract := engineering.ContractRevision{ID: "shared-api", Root: root, OwnerTaskID: "api", ConsumerTaskIDs: []string{"consumer"}, Description: "Shared API", Invariants: "Returns fixed value", Revision: 1, Sources: sources, Checks: []engineering.ContractCheck{{Name: "api-check", Tool: "bash", InputJSON: string(checkInput)}}}
	_, err = store.SaveContract(ctx, contract, 0)
	require.NoError(t, err)
	require.NoError(t, store.Update(ctx, sess.ID, func(st *engineering.State) error {
		for _, task := range sess.Todos {
			st.RoleExecutions[task.ID] = engineering.RoleExecution{TaskID: task.ID, Agent: task.Agent, Root: root, TaskFingerprint: session.TaskFingerprint(task), RequireReview: true, Handoff: &subagents.Handoff{TaskID: task.ID, Summary: "Implemented", Decision: "ready"}}
		}
		return nil
	}))
	apiCtx := engineering.WithScope(ctx, sess.ID, "api")
	failed, err := invoke(apiCtx, fantasy.ToolCall{ID: "failed-check", Name: "bash", Input: string(checkInput)})
	require.NoError(t, err)
	require.False(t, tools.ToolSucceeded("bash", failed, nil))
	st, err := store.Read(ctx, sess.ID)
	require.NoError(t, err)
	failedID := st.Operations[len(st.Operations)-1].ID
	implementation, err := invoke(apiCtx, fantasy.ToolCall{ID: "implementation", Name: "agent", Input: workflowJSON(AgentParams{AgentName: "backend"})})
	require.NoError(t, err)
	require.False(t, implementation.IsError)
	_, err = invoke(apiCtx, fantasy.ToolCall{ID: "original-review", Name: "agent", Input: workflowJSON(AgentParams{AgentName: "review"})})
	require.NoError(t, err)
	st, err = store.Read(ctx, sess.ID)
	require.NoError(t, err)
	oldSource, err := engineering.SourceFingerprint(ctx, root, store.Dir())
	require.NoError(t, err)
	finding := engineering.Finding{Root: root, TaskID: "api", TaskFingerprint: session.TaskFingerprint(sess.Todos[0]), SourceFingerprint: oldSource, Status: "open", Path: "api.txt", StartLine: 1, EndLine: 1, Severity: 1, Issue: "Broken result", Expected: "Fixed result", Evidence: "Actual process exited 7", Checks: contract.Checks}
	for _, op := range st.Operations {
		if op.CallID == "implementation" {
			finding.ImplementerExecutionID = op.ID
		}
		if op.CallID == "original-review" {
			finding.ReviewerExecutionID = op.ID
		}
	}
	finding.ID = engineering.FindingID(finding)
	findingRef, err := store.SaveFinding(ctx, sess.ID, finding, 0)
	require.NoError(t, err)
	response, err := c.repairWorkflow(ctx, WorkflowParams{Action: "repair", TaskID: "api", OperationID: failedID, RepairHypothesis: "Fix observed invalid value", Checks: checks}, fantasy.ToolCall{ID: "repair"}, invoke)
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	// Reopen the persistent runtime, retaining failures, repair and artifacts.
	store = engineering.NewStore(dataDir)
	c.engineering = store
	newSource, err := engineering.SourceFingerprint(ctx, root, store.Dir())
	require.NoError(t, err)
	require.NotEqual(t, oldSource, newSource)
	finding.Status, finding.RemediationTaskID = "fixed", finding.RemediationID()
	_, err = store.SaveFinding(ctx, sess.ID, finding, findingRef.Revision)
	require.NoError(t, err)
	response, err = c.remediationWorkflow(ctx, WorkflowParams{Action: "finding", FindingAction: "verify", FindingIDs: []string{finding.ID}}, fantasy.ToolCall{ID: "reinspect"}, invoke)
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	resolved, _, err := store.ReadFinding(ctx, sess.ID, finding.ID)
	require.NoError(t, err)
	require.False(t, store.FindingBlocks(ctx, resolved, newSource))
	require.True(t, store.FindingBlocks(ctx, resolved, oldSource))
	contract.Sources, err = engineering.CaptureSources(ctx, root, []string{"api.txt"})
	require.NoError(t, err)
	contract.Revision = 2
	ref, err := store.SaveContract(ctx, contract, 1)
	require.NoError(t, err)
	plan := &engineering.DeliveryPlan{Root: root, Profile: "feature", Requirements: []engineering.Requirement{{ID: "user", Description: "Stable API and consumer", TaskIDs: []string{"api", "consumer"}}}, Stages: []engineering.Stage{{ID: "integration", Title: "Integrated feature", TaskIDs: []string{"api", "consumer"}}}, TaskFingerprints: map[string]string{}, TaskContractRefs: map[string][]engineering.Record{}}
	for _, task := range sess.Todos {
		plan.TaskFingerprints[task.ID] = session.TaskFingerprint(task)
		plan.TaskContractRefs[task.ID] = []engineering.Record{ref}
	}
	require.NoError(t, store.Update(ctx, sess.ID, func(st *engineering.State) error { st.Delivery = plan; return nil }))
	st, err = store.Read(ctx, sess.ID)
	require.NoError(t, err)
	before, err := c.advanceDelivery(ctx, WorkflowParams{Checks: checks}, fantasy.ToolCall{ID: "premature"}, sess, st, invoke)
	require.NoError(t, err)
	require.True(t, before.IsError)
	require.False(t, st.Delivery.Stages[0].Passed)
	for i, task := range sess.Todos {
		response, err = c.contractWorkflow(ctx, WorkflowParams{ContractAction: "check", TaskID: task.ID}, fantasy.ToolCall{ID: "contract-" + task.ID}, invoke)
		require.NoError(t, err)
		require.False(t, response.IsError, response.Content)
		response, err = c.reviewTask(ctx, WorkflowParams{TaskID: task.ID, Checks: checks}, fantasy.ToolCall{ID: "quality-" + task.ID}, sess, invoke)
		require.NoError(t, err)
		require.False(t, response.IsError, response.Content)
		sess.Todos[i].Status, sess.Todos[i].Verification = session.TodoStatusCompleted, "passed"
		sess.Todos[i].Evidence = []session.TodoEvidence{{Kind: "command", Detail: "Real fixture check exited 0"}}
	}
	sess, err = env.sessions.Save(ctx, sess)
	require.NoError(t, err)
	st, err = store.Read(ctx, sess.ID)
	require.NoError(t, err)
	response, err = c.advanceDelivery(ctx, WorkflowParams{Checks: checks}, fantasy.ToolCall{ID: "final-stage"}, sess, st, invoke)
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	st, err = store.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.True(t, st.Delivery.Stages[0].Passed)
	cp := engineering.Checkpoint{ID: uuid.NewString(), Root: root, SessionID: sess.ID, PlanFingerprint: st.Delivery.Fingerprint(), SourceFingerprint: newSource, TaskFingerprints: plan.TaskFingerprints, Stage: 1}
	_, err = store.SaveCheckpoint(ctx, cp)
	require.NoError(t, err)
	local, err := c.WorkflowSnapshot(ctx, sess.ID)
	require.NoError(t, err)
	shared, err := ReadWorkflowSnapshot(ctx, engineering.NewStore(dataDir), env.sessions, root, sess.ID, false)
	require.NoError(t, err)
	require.Equal(t, local, shared)
	require.GreaterOrEqual(t, len(shared.Checkpoints), 1)
	require.NoError(t, os.WriteFile(source, []byte("regressed"), 0o600))
	_, err = c.deliveryReady(ctx, sess.ID, sess.Todos, 2)
	require.Error(t, err, "Historical pass must not certify changed source")
}
