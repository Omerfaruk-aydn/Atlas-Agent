package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// TestBrowserPipelineUsesSameHookPathAsDirectCalls mirrors the coordinator
// wiring: pipeline children are dispatched to the same hook-wrapped tool
// list as direct calls, so a PreToolUse hook sees and can deny both with
// the same tool name and input shape.
func TestBrowserPipelineUsesSameHookPathAsDirectCalls(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.ToSlash(filepath.Join(dir, "hook.log"))
	runner := newRunner(t, fmt.Sprintf(`cat >> %q; echo >> %q; echo "blocked by policy" >&2; exit 2`, logPath, logPath))
	browserTool := tools.NewBrowserTool(permission.NewPermissionService(dir, true, nil), dir, config.ToolBrowser{})
	wrapped := wrapToolsWithHooks([]fantasy.AgentTool{browserTool}, runner, nil, false)
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		for _, tool := range wrapped {
			if tool.Info().Name == call.Name {
				return tool.Run(ctx, call)
			}
		}
		return fantasy.NewTextErrorResponse("unavailable"), nil
	}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "hook-parity")
	direct, err := invoke(ctx, fantasy.ToolCall{ID: "direct", Name: tools.BrowserToolName, Input: `{"action":"semantic_click","advanced":{"role":"button","name":"Save"}}`})
	require.NoError(t, err)
	require.True(t, direct.IsError)
	require.Contains(t, direct.Content, "blocked by policy")

	pipeline := tools.NewToolPipeline(invoke)
	piped, err := pipeline.Run(ctx, fantasy.ToolCall{ID: "pipe", Input: `{"browser":{"steps":[{"id":"save","action":"semantic_click","target":{"role":"button","name":"Save"}},{"id":"after","action":"observe"}]}}`})
	require.NoError(t, err)
	require.True(t, piped.IsError)
	require.Contains(t, piped.Content, "blocked by policy")
	require.Contains(t, piped.Content, `"id":"after","iteration":0,"status":"skipped"`, "a denied step stops the rest")

	data, err := os.ReadFile(filepath.FromSlash(logPath))
	require.NoError(t, err)
	payloads := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, payloads, 2, "the hook ran once for the direct call and once for the pipeline child")
	for _, payload := range payloads {
		require.Contains(t, payload, `"tool_name":"browser"`)
		require.Contains(t, payload, `"action":"semantic_click"`)
		require.Contains(t, payload, `"name":"Save"`)
	}
}

// completionDecision adapts the end-of-turn decision to (prompt, continue).
func completionDecision(ctx context.Context, a *sessionAgent, session, prompt string, result *fantasy.AgentResult, queued int) (string, bool) {
	d := a.browserCompletionPrompt(ctx, session, prompt, result, queued)
	return d.Prompt, d.Continue
}

func completionAgent(t *testing.T) *sessionAgent {
	t.Helper()
	a := &sessionAgent{workingDir: t.TempDir(), maxStepsPerTurn: csync.NewValue(100)}
	return a
}

// endTurnResult is a finished turn whose browser calls used the given
// actions; stop marks a handoff or permission stop on its last step.
func endTurnResult(stop bool, actions ...string) *fantasy.AgentResult {
	step := fantasy.StepResult{}
	for i, action := range actions {
		step.Content = append(step.Content, fantasy.ToolCallContent{ToolCallID: fmt.Sprintf("c%d", i), ToolName: tools.BrowserToolName, Input: fmt.Sprintf(`{"action":%q}`, action)})
	}
	if stop {
		step.Content = append(step.Content, fantasy.ToolResultContent{ToolCallID: "x", ToolName: "browser", StopTurn: true})
	}
	return &fantasy.AgentResult{Steps: []fantasy.StepResult{step}}
}

func TestBrowserCompletionGateContinuesOnlyNormalTurns(t *testing.T) {
	t.Parallel()
	a := completionAgent(t)
	session := "completion-" + t.Name()
	tool := tools.NewBrowserTool(permission.NewPermissionService(a.workingDir, true, nil), a.workingDir, config.ToolBrowser{})
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, session)
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "plan", Name: tools.BrowserToolName, Input: `{"action":"task_plan","task":{"task_id":"cards","requirements":[{"id":"cards","text":"Add the task cards"}]}}`})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	worked := endTurnResult(false, "task_plan", "snapshot")

	_, ok := completionDecision(t.Context(), a, session, "build it", worked, 1)
	require.False(t, ok, "a prompt the user queued goes first")
	_, ok = completionDecision(t.Context(), a, session, "build it", endTurnResult(true, "task_plan"), 0)
	require.False(t, ok, "a handoff or permission stop is not continued")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, ok = completionDecision(canceled, a, session, "build it", worked, 0)
	require.False(t, ok, "cancellation ends the work")
	a.maxStepsPerTurn = csync.NewValue(1)
	_, ok = completionDecision(t.Context(), a, session, "build it", worked, 0)
	require.False(t, ok, "reaching the step limit is an explicit bound")
	a.maxStepsPerTurn = csync.NewValue(100)

	// The user moved on: a turn they started that never touched the open
	// contract is not continued into the old task.
	_, ok = completionDecision(t.Context(), a, session, "now summarize the README instead", endTurnResult(false, "snapshot"), 0)
	require.False(t, ok, "a new user direction does not restart the old task")
	_, ok = completionDecision(t.Context(), a, session, "now summarize the README instead", &fantasy.AgentResult{Steps: []fantasy.StepResult{{}}}, 0)
	require.False(t, ok)

	prompt, ok := completionDecision(t.Context(), a, session, "build it", worked, 0)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(prompt, tools.BrowserContinuationMarker))
	require.Contains(t, prompt, "cards (pending)")
	require.Contains(t, prompt, "without asking for confirmation")
	// A continuation turn continues on its own marker, without new contract
	// calls, until the task's own progress bound stops it.
	_, ok = completionDecision(t.Context(), a, session, prompt, endTurnResult(false, "snapshot"), 0)
	require.True(t, ok)
	_, ok = completionDecision(t.Context(), a, session, prompt, endTurnResult(false, "snapshot"), 0)
	require.False(t, ok, "continuations without progress stop")

	interaction.Default.Pause(session, "approve on phone")
	defer interaction.Default.Resume(session)
	_, ok = completionDecision(t.Context(), a, session, prompt, worked, 0)
	require.False(t, ok, "work waiting for the user is not resumed automatically")
}

func TestBrowserCompletionSurvivesCompactionOfFullDesignRequest(t *testing.T) {
	t.Parallel()
	a := completionAgent(t)
	session := "compact-design-" + t.Name()
	planContract(t, a, session, designContract)
	original := "Finish the mobile interface, verify all requested screens and play both prototype paths."
	resumed := summaryResumeAfterTurn(original, endTurnResult(false, "task_plan", "set_field"))
	require.True(t, strings.HasPrefix(resumed, tools.BrowserContinuationMarker))
	_, ok := completionDecision(t.Context(), a, session, resumed, endTurnResult(false, "screenshot"), 0)
	require.True(t, ok, "compaction must not forget that the full request worked on the contract")

	unrelated := summaryResumeAfterTurn("Summarize the README instead.", endTurnResult(false, "snapshot"))
	require.False(t, strings.HasPrefix(unrelated, tools.BrowserContinuationMarker))
	_, ok = completionDecision(t.Context(), a, session, unrelated, endTurnResult(false, "snapshot"), 0)
	require.False(t, ok, "an unrelated request still does not inherit the design task")
}

// planContract registers a contract through the real browser tool, the way
// the agent does, and returns the tool for later calls.
func planContract(t *testing.T, a *sessionAgent, session, plan string) fantasy.AgentTool {
	t.Helper()
	tool := tools.NewBrowserTool(permission.NewPermissionService(a.workingDir, true, nil), a.workingDir, config.ToolBrowser{})
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, session)
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "plan", Name: tools.BrowserToolName, Input: plan})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	return tool
}

const designContract = `{"action":"task_plan","task":{"task_id":"atlas-remote-mobile","requirements":[{"id":"chat","text":"390x844 chat screen"},{"id":"components","text":"Real components with states","verify":"structural"},{"id":"prototype","text":"Chat to question to chat prototype links","verify":"structural"}]}}`

// TestBrowserCompletionResumesOnUserContinue replays session aaae8f0a:
// the user answered "devam et", the turn did not call task_plan or
// task_update, and the agent ended with "I can keep going if you want"
// while 14 of 15 requirements were open. A continue directive is the
// already authorized task, so the gate must resume it.
func TestBrowserCompletionResumesOnUserContinue(t *testing.T) {
	t.Parallel()
	a := completionAgent(t)
	session := "continue-" + t.Name()
	planContract(t, a, session, designContract)
	stoppedAsking := endTurnResult(false, "find", "screenshot")
	for _, prompt := range []string{"devam et", "Devam et.", "continue", "Kaldığın yerden devam et!"} {
		require.True(t, IsContinueDirective(prompt), prompt)
	}
	next, ok := completionDecision(t.Context(), a, session, "devam et", stoppedAsking, 0)
	require.True(t, ok, "a continue directive resumes the open contract")
	require.Contains(t, next, "prototype (pending)")
	require.Contains(t, next, "without asking for confirmation")

	// Auto-compaction re-queues the prompt wrapped; the wrapper must not
	// hide that it continues the contract.
	wrapped := summaryResumePrompt("devam et")
	require.Equal(t, "devam et", unwrapSummaryResume(wrapped))
	_, ok = completionDecision(t.Context(), a, session, wrapped, stoppedAsking, 0)
	require.True(t, ok, "the compaction-resumed continue turn keeps going")
	marker := summaryResumePrompt(next)
	require.True(t, strings.HasPrefix(marker, tools.BrowserContinuationMarker), "a wrapped completion check keeps its marker")

	// New work is not mistaken for "continue".
	for _, prompt := range []string{"now summarize the README instead", "devam etme, dur", "continue with the onboarding screen instead of chat"} {
		require.False(t, IsContinueDirective(prompt), prompt)
	}
}

// TestBrowserTaskSurvivesModelChangeAndCompaction: the contract is read
// from its checkpoint on every request, so a different model after a
// fallback or a compacted history still sees the goal, the requirement
// states and the remaining work.
func TestBrowserTaskSurvivesModelChangeAndCompaction(t *testing.T) {
	t.Parallel()
	a := completionAgent(t)
	session := "survive-" + t.Name()
	planContract(t, a, session, designContract)
	block := tools.BrowserTaskPromptBlock(a.workingDir, session)
	require.Contains(t, block, `<browser_task id="atlas-remote-mobile" open=3`)
	require.Contains(t, block, "- components [pending, needs structural evidence]")
	require.Contains(t, block, "without asking again")
	require.Contains(t, block, "an existing frame or layer is not evidence")

	tools.RecordBrowserTaskStop(a.workingDir, session, tools.BrowserStopProviderError, "Too Many Requests: The usage limit has been reached")
	block = tools.BrowserTaskPromptBlock(a.workingDir, session)
	require.Contains(t, block, "Last stop: provider_error: Too Many Requests")
	require.Equal(t, block, tools.BrowserTaskPromptBlock(a.workingDir, session), "the block depends only on the checkpoint, never on the model")
}

// TestBrowserCompletionSeparatesStopReasons: cancellation, waiting for the
// user and a lack of progress are recorded as different reasons, and none
// of them is continued.
func TestBrowserCompletionSeparatesStopReasons(t *testing.T) {
	t.Parallel()
	status := func(t *testing.T, a *sessionAgent, session string) string {
		t.Helper()
		return tools.BrowserTaskPromptBlock(a.workingDir, session)
	}
	t.Run("canceled", func(t *testing.T) {
		t.Parallel()
		a := completionAgent(t)
		session := "cancel-" + t.Name()
		planContract(t, a, session, designContract)
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		d := a.browserCompletionPrompt(canceled, session, "devam et", endTurnResult(false, "screenshot"), 0)
		require.False(t, d.Continue)
		require.Contains(t, status(t, a, session), "Last stop: user_canceled")
	})
	t.Run("waiting", func(t *testing.T) {
		t.Parallel()
		a := completionAgent(t)
		session := "wait-" + t.Name()
		planContract(t, a, session, designContract)
		d := a.browserCompletionPrompt(t.Context(), session, "devam et", endTurnResult(true, "handoff"), 0)
		require.False(t, d.Continue)
		require.Contains(t, status(t, a, session), "Last stop: waiting_for_user")
	})
	t.Run("no_progress", func(t *testing.T) {
		t.Parallel()
		a := completionAgent(t)
		session := "progress-" + t.Name()
		planContract(t, a, session, designContract)
		prompt, ok := completionDecision(t.Context(), a, session, "devam et", endTurnResult(false, "screenshot"), 0)
		require.True(t, ok)
		// Only screenshots and observations: not progress.
		_, ok = completionDecision(t.Context(), a, session, prompt, endTurnResult(false, "screenshot", "observe"), 0)
		require.True(t, ok, "one continuation without progress is allowed")
		d := a.browserCompletionPrompt(t.Context(), session, prompt, endTurnResult(false, "screenshot"), 0)
		require.False(t, d.Continue)
		require.Contains(t, d.StopReason, "no_progress")
		require.Equal(t, 3, d.Open)
		require.Contains(t, status(t, a, session), "Last stop: no_progress")
	})
}
