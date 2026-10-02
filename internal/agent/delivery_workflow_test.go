package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestDeliveryPlanGatesWavesAndRejectsInventedOrMutatingVerification(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "feature.go"), []byte("package fixture\n"), 0o644))
	cfg, err := config.Init(root, "", false)
	require.NoError(t, err)
	store := engineering.NewStore(t.TempDir())
	c := &coordinator{cfg: cfg, sessions: env.sessions, engineering: store}
	sess, err := env.sessions.Create(t.Context(), "delivery")
	require.NoError(t, err)
	sess.Todos = []session.Todo{
		{ID: "later", Content: "Expand", Status: session.TodoStatusPending, Agent: "backend", OwnedPaths: []string{"later"}, AcceptanceCriteria: []string{"Expansion works"}},
		{ID: "slice", Content: "Vertical slice", Status: session.TodoStatusPending, Agent: "backend", OwnedPaths: []string{"."}, AcceptanceCriteria: []string{"Slice works"}},
	}
	_, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	plan := &engineering.DeliveryPlan{Profile: "feature", Requirements: []engineering.Requirement{{ID: "user-feature", Description: "Working feature", TaskIDs: []string{"slice", "later"}}}, Stages: []engineering.Stage{{ID: "slice", Title: "Vertical slice", TaskIDs: []string{"slice"}, Passed: true}, {ID: "expand", Title: "Expansion", TaskIDs: []string{"later"}}}}
	response, err := c.deliveryWorkflow(ctx, WorkflowParams{Action: "plan", Plan: plan}, fantasy.ToolCall{ID: "plan"}, sess, nil)
	require.NoError(t, err)
	require.False(t, response.IsError)
	wave, err := c.deliveryReady(ctx, sess.ID, sess.Todos, 4)
	require.NoError(t, err)
	require.Len(t, wave, 1)
	require.Equal(t, "slice", wave[0].ID)
	st, err := store.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.False(t, st.Delivery.Stages[0].Passed)
	sess.Todos[1].Status = session.TodoStatusCompleted
	sess.Todos[1].Verification = "passed"
	sess.Todos[1].Evidence = []session.TodoEvidence{{Kind: "command", Detail: "Slice check passed"}}
	_, err = env.sessions.Save(ctx, sess)
	require.NoError(t, err)
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse(`{"passed":true}`), nil
	}
	response, err = c.advanceDelivery(ctx, WorkflowParams{}, fantasy.ToolCall{ID: "advance"}, sess, st, invoke)
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "machine-observed")
	invoke = func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		scope := engineering.GetScope(ctx, "")
		require.NoError(t, store.Update(ctx, sess.ID, func(st *engineering.State) error {
			st.Checks = append(st.Checks, engineering.Check{TaskID: scope.TaskID, RunID: call.ID, Passed: true, Evidence: "Observed exit 0", CheckedAt: time.Now().UnixMilli()})
			return nil
		}))
		return fantasy.NewTextResponse(`{"passed":true}`), nil
	}
	mutating := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		response, err := invoke(ctx, call)
		require.NoError(t, os.WriteFile(filepath.Join(root, "feature.go"), []byte("package changed\n"), 0o644))
		return response, err
	}
	response, err = c.advanceDelivery(ctx, WorkflowParams{}, fantasy.ToolCall{ID: "mutating"}, sess, st, mutating)
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "source changed")
	require.NoError(t, os.WriteFile(filepath.Join(root, "feature.go"), []byte("package fixture\n"), 0o644))
	response, err = c.advanceDelivery(ctx, WorkflowParams{}, fantasy.ToolCall{ID: "observed"}, sess, st, invoke)
	require.NoError(t, err)
	require.False(t, response.IsError)
	restarted := engineering.NewStore(filepath.Dir(store.Dir()))
	st, err = restarted.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, 1, st.Delivery.CurrentStage)
	require.True(t, st.Delivery.Stages[0].Passed)
	wave, err = c.deliveryReady(ctx, sess.ID, sess.Todos, 4)
	require.NoError(t, err)
	require.Len(t, wave, 1)
	require.Equal(t, "later", wave[0].ID)
	sess.Todos[0].Content = "Changed requirement"
	_, err = c.deliveryReady(ctx, sess.ID, sess.Todos, 4)
	require.ErrorContains(t, err, "changed")
}

func TestDeliveryContextRemainsBoundedWithLargePersistentKnowledge(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	refs, err := engineering.CaptureSources(t.Context(), root, []string{"go.mod"})
	require.NoError(t, err)
	record := engineering.KnowledgeRecord{ID: "large", Kind: "decision", Context: strings.Repeat("c", 2048), Decision: strings.Repeat("d", 4096), Sources: refs}
	for range 16 {
		record.Alternatives = append(record.Alternatives, strings.Repeat("a", 1024))
		record.Consequences = append(record.Consequences, strings.Repeat("r", 1024))
	}
	require.NoError(t, store.SaveKnowledge(t.Context(), root, record))
	a := &sessionAgent{engineering: store, workingDir: root}
	text, err := a.deliveryContext(t.Context(), "session", "Implement feature", nil)
	require.NoError(t, err)
	require.Less(t, len(text), 25*1024)
	require.Contains(t, text, `"details_omitted":true`)
	require.Contains(t, text, "workflow trace/knowledge")
}

func TestRequirementTraceDoesNotAdvertiseAnOldReviewForChangedTask(t *testing.T) {
	t.Parallel()
	old := session.Todo{ID: "api", Content: "Old behavior"}
	revised := old
	revised.Content = "Revised behavior"
	st := engineering.State{Delivery: &engineering.DeliveryPlan{Requirements: []engineering.Requirement{{ID: "user", TaskIDs: []string{"api"}}}, TaskFingerprints: map[string]string{"api": session.TaskFingerprint(revised)}}, RoleExecutions: map[string]engineering.RoleExecution{"api": {TaskFingerprint: session.TaskFingerprint(old), Passed: true}}}
	data, err := json.Marshal(deliveryTrace(st, []session.Todo{revised}))
	require.NoError(t, err)
	require.Contains(t, string(data), `"handoff_matches_task":false`)
	require.Contains(t, string(data), `"independent_review_passed":false`)
}

func TestDeliveryContextPreparesProjectAndPreservesProfileAcrossSteering(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	a := &sessionAgent{engineering: store, workingDir: root}
	text, err := a.deliveryContext(t.Context(), "session", "Frontend arayüz tasarımı", nil)
	require.NoError(t, err)
	require.Contains(t, text, `"name":"ui"`)
	require.Contains(t, text, "go.mod")
	text, err = a.deliveryContext(t.Context(), "session", "Fix one failure", nil)
	require.NoError(t, err)
	require.Contains(t, text, `"name":"ui"`)
}

func TestVerifiedLessonRejectsUnsupportedClaimsAndPersistsObservedRepair(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "feature.go"), []byte("package fixture\n"), 0o644))
	cfg, err := config.Init(root, "", false)
	require.NoError(t, err)
	store := engineering.NewStore(t.TempDir())
	c := &coordinator{cfg: cfg, sessions: env.sessions, engineering: store}
	sess, err := env.sessions.Create(t.Context(), "lesson")
	require.NoError(t, err)
	op, err := store.Begin(t.Context(), sess.ID, "failure", "bash", `{}`, "task")
	require.NoError(t, err)
	require.NoError(t, store.Finish(t.Context(), sess.ID, op, false, false))
	record := &engineering.KnowledgeRecord{ID: "repair", Context: "Failure mechanism", Decision: "Verified repair", Consequences: []string{"Preserve invariant"}, Sources: []engineering.SourceReference{{Path: "feature.go"}}, FailureOperationID: op, VerificationRunID: "invented"}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	invoke := func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse(`{"passed":true}`), nil
	}
	resp, err := c.deliveryWorkflow(ctx, WorkflowParams{Action: "lesson", Knowledge: record}, fantasy.ToolCall{ID: "unsupported"}, sess, invoke)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	invoke = func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		scope := engineering.GetScope(ctx, "")
		err := store.Update(ctx, sess.ID, func(st *engineering.State) error {
			st.Checks = append(st.Checks, engineering.Check{TaskID: scope.TaskID, RunID: call.ID, Passed: true, Evidence: "Observed checker exit 0", CheckedAt: time.Now().UnixMilli()})
			return nil
		})
		return fantasy.NewTextResponse(`{"passed":true}`), err
	}
	resp, err = c.deliveryWorkflow(ctx, WorkflowParams{Action: "lesson", Knowledge: record}, fantasy.ToolCall{ID: "repair"}, sess, invoke)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	_, records, err := store.ProjectKnowledge(ctx, root)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "repair-lesson-verify", records[0].Record.VerificationRunID)
	require.Len(t, records[0].Record.Evidence, 1)
}
