package agent

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

func TestGuardStopsRepeatedFailuresBeforeExecutingAgain(t *testing.T) {
	s := engineering.NewStore(t.TempDir())
	calls := 0
	inner := fantasy.NewAgentTool("test_run", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("tests failed"), tools.TestRunResponseMetadata{OK: false}), nil
	})
	guard := &guardedTool{AgentTool: inner, store: s}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")
	for range 3 {
		resp, err := guard.Run(ctx, fantasy.ToolCall{ID: "c", Name: "test_run", Input: "{}"})
		require.NoError(t, err)
		require.False(t, resp.StopTurn)
	}
	resp, err := guard.Run(ctx, fantasy.ToolCall{ID: "c", Name: "test_run", Input: "{}"})
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	require.Equal(t, 3, calls)
}

func TestGuardSemanticRecoveryAccountsForMutationAfterBudgetExhaustion(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "session")
	require.NoError(t, store.SetBudget(ctx, "session", "", engineering.Limits{MaxToolCalls: 1}))
	_, err := store.Begin(ctx, "session", "old", "bash", "old", "")
	require.NoError(t, err)
	require.Error(t, store.Check(ctx, "session", ""))
	inner := fantasy.NewAgentTool(tools.LSPEditPlanToolName, "test", func(context.Context, tools.EditPlanParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("restored"), struct {
			Mutation bool `json:"semantic_mutation"`
		}{true}), nil
	})
	response, err := (&guardedTool{AgentTool: inner, store: store}).Run(ctx, fantasy.ToolCall{Name: tools.LSPEditPlanToolName, Input: `{"action":"recover","plan_id":"plan"}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	state, err := store.Read(ctx, "session")
	require.NoError(t, err)
	require.EqualValues(t, 1, state.Generation)
	require.Len(t, state.Operations, 1, "recovery must not replay or admit implementation work")
}

func TestOwnershipRejectsEscapesAndAnotherTasksFiles(t *testing.T) {
	root := t.TempDir()
	scope := engineering.Scope{SessionID: "s", WriteRoot: root, OwnedPaths: []string{"src"}}
	require.NoError(t, checkToolOwnership(scope, fantasy.ToolCall{Name: "write", Input: workflowJSON(tools.WriteParams{FilePath: filepath.Join(root, "src", "new.go")})}))
	require.ErrorContains(t, checkToolOwnership(scope, fantasy.ToolCall{Name: "write", Input: workflowJSON(tools.WriteParams{FilePath: filepath.Join(root, "docs", "new.md")})}), "ownership")
	require.ErrorContains(t, checkToolOwnership(scope, fantasy.ToolCall{Name: "write", Input: workflowJSON(tools.WriteParams{FilePath: filepath.Join(root, "..", "outside.go")})}), "escapes")
}

func TestReadonlySpecialistCannotOptIntoExecutionOrDelegation(t *testing.T) {
	sub := &subagents.Subagent{ReadOnly: true, Tools: []string{"view", "bash", "write", "agent", "verify"}}
	require.Equal(t, []string{"view"}, subagentAllowedTools(sub, sub.Tools))
}

func TestSpecialistCommandsRespectBothAllowlists(t *testing.T) {
	t.Parallel()
	commands := []string{"view", "bash", "test_run", "lint_run", "verify", "job_output", "job_kill"}
	primary := append(append([]string{}, commands...), "write", "edit", "agent", "workflow", "worktree", "mcp_custom")
	sub := &subagents.Subagent{ReadOnly: true, AllowCommands: true}
	require.Equal(t, commands, subagentAllowedTools(sub, primary))
	sub.Tools = []string{"bash", "verify", "write", "agent", "mcp_custom"}
	require.Equal(t, []string{"bash", "verify"}, subagentAllowedTools(sub, primary))
	require.Equal(t, []string{"verify"}, subagentAllowedTools(sub, []string{"verify", "write"}))
}

func TestAuxiliaryUsageChargesRootAndStopsNextProviderStep(t *testing.T) {
	t.Parallel()
	s := engineering.NewStore(t.TempDir())
	a := &sessionAgent{engineering: s}
	ctx := engineering.WithScope(t.Context(), "root", "review")
	require.NoError(t, s.SetBudget(ctx, "root", "", engineering.Limits{MaxTokens: 15}))
	call := a.engineeringReviewCall(ctx, "child", Model{}, "review")
	_, _, err := call.PrepareStep(ctx, fantasy.PrepareStepFunctionOptions{})
	require.NoError(t, err)
	require.NoError(t, call.OnStepFinish(fantasy.StepResult{Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 8, OutputTokens: 2, CacheReadTokens: 5}}}))
	_, _, err = call.PrepareStep(ctx, fantasy.PrepareStepFunctionOptions{})
	require.ErrorContains(t, err, "exhausted")
	state, err := s.Read(ctx, "root")
	require.NoError(t, err)
	require.EqualValues(t, 15, state.Usage.Tokens)
	require.EqualValues(t, 15, state.Tasks["review"].Usage.Tokens)
	require.EqualValues(t, 20, engineeringTokens(fantasy.Usage{TotalTokens: 20, InputTokens: 8, CacheReadTokens: 5}))
}

func TestBackgroundJournalClosesOnlyOnObservedJobExit(t *testing.T) {
	t.Parallel()
	s := engineering.NewStore(t.TempDir())
	ctx := engineering.WithScope(t.Context(), "s", "task")
	bash := fantasy.NewAgentTool("bash", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("started"), tools.BashResponseMetadata{Background: true, ShellID: "job-1"}), nil
	})
	_, err := (&guardedTool{AgentTool: bash, store: s}).Run(ctx, fantasy.ToolCall{ID: "b", Name: "bash", Input: "{}"})
	require.NoError(t, err)
	state, err := s.Read(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, "running", state.Operations[0].Status)
	require.Equal(t, "job-1", state.Operations[0].BackgroundID)
	code := 1
	job := fantasy.NewAgentTool("job_output", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("failed"), tools.JobOutputResponseMetadata{Done: true, ShellID: "job-1", ExitCode: &code}), nil
	})
	_, err = (&guardedTool{AgentTool: job, store: s}).Run(ctx, fantasy.ToolCall{ID: "j", Name: "job_output", Input: "{}"})
	require.NoError(t, err)
	state, err = s.Read(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, "failed", state.Operations[0].Status)
}

type deadlineTitleModel struct {
	fastModel
	calls atomic.Int32
}

func (m *deadlineTitleModel) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDetachedTitleStillRespectsActiveTimeBudget(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	model := &deadlineTitleModel{}
	a := testSessionAgent(env, fastModel{}, model, "system").(*sessionAgent)
	a.engineering = engineering.NewStore(t.TempDir())
	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	require.NoError(t, a.engineering.SetBudget(t.Context(), sess.ID, "", engineering.Limits{MaxDurationMS: 30}))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	a.GenerateTitle(ctx, sess.ID, "A real title prompt")
	require.Less(t, time.Since(started), time.Second)
	require.EqualValues(t, 1, model.calls.Load())
	state, err := a.engineering.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, state.Usage.DurationMS, int64(20))
	updated, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, DefaultSessionName, updated.Title)
}

func TestCanceledErrorResponseLeavesEffectsUnresolved(t *testing.T) {
	t.Parallel()
	s := engineering.NewStore(t.TempDir())
	ctx, cancel := context.WithCancel(engineering.WithScope(t.Context(), "s", "task"))
	defer cancel()
	inner := fantasy.NewAgentTool("bash", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		cancel()
		return fantasy.NewTextErrorResponse("Command interrupted; partial effects are unknown"), nil
	})
	resp, err := (&guardedTool{AgentTool: inner, store: s}).Run(ctx, fantasy.ToolCall{ID: "c", Name: "bash", Input: "{}"})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	state, err := s.Read(t.Context(), "s")
	require.NoError(t, err)
	require.Len(t, state.Operations, 1)
	require.Equal(t, "running", state.Operations[0].Status)
}
