package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codegraph"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestContractSourcesAndDispatch(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("package api\n"), 0o600))
	sess, err := env.sessions.Create(t.Context(), "contracts")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "api", Content: "Implement API", Status: session.TodoStatusPending, Agent: "backend", OwnedPaths: []string{"."}, AcceptanceCriteria: []string{"API works"}}}
	_, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(root), sessions: env.sessions, engineering: engineering.NewStore(t.TempDir()), permissions: env.permissions}
	env.permissions.SetMode(permission.ModeBypass)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	contract := &engineering.ContractRevision{ID: "api", Root: "forged", OwnerTaskID: "api", Description: "Public API", Invariants: "Stable response", Revision: 1, Sources: []engineering.SourceReference{{Path: "api.go", Fingerprint: "invented"}}, Checks: []engineering.ContractCheck{{Name: "api-check", Tool: "bash", InputJSON: `{"command":"true"}`}}}
	resp, err := c.contractWorkflow(ctx, WorkflowParams{ContractAction: "register", Contract: contract}, fantasy.ToolCall{ID: "register"}, nil)
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	packet, err := prepareTaskContext(ctx, c.cfg, c.engineering, sess.Todos[0], codegraph.CodeGraph{})
	require.NoError(t, err)
	require.Len(t, packet.ContractRefs, 1)
	require.Len(t, packet.Contracts, 1)
	require.Equal(t, root, packet.Contracts[0].Root)
	require.Equal(t, engineering.Hash("package api\n"), packet.Contracts[0].Sources[0].Fingerprint)
	plan := &engineering.DeliveryPlan{Profile: "feature", Requirements: []engineering.Requirement{{ID: "user", Description: "API", TaskIDs: []string{"api"}}}, Stages: []engineering.Stage{{ID: "api", Title: "API", TaskIDs: []string{"api"}}}, TaskContractRefs: map[string][]engineering.Record{"api": {{Revision: 999}}}}
	resp, err = c.deliveryWorkflow(ctx, WorkflowParams{Action: "plan", Plan: plan}, fantasy.ToolCall{ID: "plan"}, sess, nil)
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	st, err := c.engineering.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, packet.ContractRefs, st.Delivery.TaskContractRefs["api"])
	contract.ConsumerTaskIDs = []string{"unknown"}
	contract.Revision = 2
	resp, err = c.contractWorkflow(ctx, WorkflowParams{ContractAction: "revise", Contract: contract}, fantasy.ToolCall{ID: "bad-consumer"}, nil)
	require.NoError(t, err)
	require.True(t, resp.IsError)
}

func TestContractStageRequiresCurrentCheck(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("package api\n"), 0o600))
	sess, err := env.sessions.Create(t.Context(), "contract-check")
	require.NoError(t, err)
	task := session.Todo{ID: "api", Content: "Implement", Status: session.TodoStatusCompleted, Verification: "passed", Evidence: []session.TodoEvidence{{Kind: "command", Detail: "Executed"}}, Agent: "backend", OwnedPaths: []string{"."}, AcceptanceCriteria: []string{"API works"}}
	sess.Todos = []session.Todo{task}
	_, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(root), sessions: env.sessions, engineering: engineering.NewStore(t.TempDir()), permissions: env.permissions}
	env.permissions.SetMode(permission.ModeBypass)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	sources, err := engineering.CaptureSources(ctx, root, []string{"api.go"})
	require.NoError(t, err)
	contract := engineering.ContractRevision{ID: "api", Root: root, OwnerTaskID: task.ID, Description: "Public API", Invariants: "Stable response", Revision: 1, Sources: sources, Checks: []engineering.ContractCheck{{Name: "api-check", Tool: "bash", InputJSON: `{"command":"true"}`}}}
	ref, err := c.engineering.SaveContract(ctx, contract, 0)
	require.NoError(t, err)
	plan := &engineering.DeliveryPlan{Root: root, Profile: "feature", Requirements: []engineering.Requirement{{ID: "user", Description: "API", TaskIDs: []string{task.ID}}}, Stages: []engineering.Stage{{ID: "api", Title: "API", TaskIDs: []string{task.ID}}}, TaskFingerprints: map[string]string{task.ID: session.TaskFingerprint(task)}, TaskContractRefs: map[string][]engineering.Record{task.ID: {ref}}}
	require.NoError(t, c.engineering.Update(ctx, sess.ID, func(st *engineering.State) error { st.Delivery = plan; return nil }))
	st, err := c.engineering.Read(ctx, sess.ID)
	require.NoError(t, err)
	resp, err := c.advanceDelivery(ctx, WorkflowParams{}, fantasy.ToolCall{ID: "stale"}, sess, st, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		t.Fatal("stage must block before running")
		return fantasy.ToolResponse{}, nil
	})
	require.NoError(t, err)
	require.True(t, resp.IsError)

	var invoke tools.ToolInvoker
	bash := fantasy.NewAgentTool("bash", "fixture", func(ctx context.Context, p tools.BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		zero := 0
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Executed"), tools.BashResponseMetadata{ExitCode: &zero, Output: "Executed"}), nil
	})
	verify := tools.NewVerifyTool(root, c.engineering, func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return invoke(ctx, call)
	})
	invoke = func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		tool := bash
		if call.Name == "verify" {
			tool = verify
		}
		return (&guardedTool{AgentTool: tool, store: c.engineering}).Run(ctx, call)
	}
	resp, err = c.contractWorkflow(ctx, WorkflowParams{ContractAction: "check", TaskID: task.ID}, fantasy.ToolCall{ID: "current"}, invoke)
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.NoError(t, c.engineering.ValidateTaskContracts(ctx, sess.ID, root, task.ID, session.TaskFingerprint(task), []engineering.Record{ref}))
	resp, err = c.advanceDelivery(ctx, WorkflowParams{Checks: []tools.VerificationStep{{Name: "api-check", Tool: "bash", Input: json.RawMessage(contract.Checks[0].InputJSON)}}}, fantasy.ToolCall{ID: "verified-stage"}, sess, st, invoke)
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	contract.Revision = 2
	contract.Invariants = "New response shape"
	_, err = c.engineering.SaveContract(ctx, contract, 1)
	require.NoError(t, err)
	require.Error(t, c.engineering.ValidateTaskContracts(ctx, sess.ID, root, task.ID, session.TaskFingerprint(task), []engineering.Record{ref}))
	st, err = c.engineering.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.True(t, st.Delivery.Stages[0].Passed, "previous stage remains historical")
	_, err = c.deliveryReady(ctx, sess.ID, sess.Todos, 1)
	require.Error(t, err, "historical stage cannot authorize work with a revised contract")
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("package changed\n"), 0o600))
	packet, err := prepareTaskContext(ctx, c.cfg, c.engineering, task, codegraph.CodeGraph{})
	require.NoError(t, err, "stale contract evidence must not prevent corrective agent turns")
	require.NotEmpty(t, packet.Gaps)
	data, err := json.Marshal(st.Delivery)
	require.NoError(t, err)
	require.NotEmpty(t, data)
}
