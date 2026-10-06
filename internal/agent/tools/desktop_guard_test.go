package tools

import (
	"context"

	"sync/atomic"
	"testing"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// scriptedTool answers each call from a queue so guard behaviour is observable
// without any desktop.
type scriptedTool struct {
	name    string
	replies []fantasy.ToolResponse
	errs    []error
	calls   int32
}

func (s *scriptedTool) Info() fantasy.ToolInfo                     { return fantasy.ToolInfo{Name: s.name} }
func (s *scriptedTool) ProviderOptions() fantasy.ProviderOptions   { return nil }
func (s *scriptedTool) SetProviderOptions(fantasy.ProviderOptions) {}
func (s *scriptedTool) Run(_ context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	i := int(atomic.AddInt32(&s.calls, 1)) - 1
	var err error
	if i < len(s.errs) {
		err = s.errs[i]
	}
	if i < len(s.replies) {
		return s.replies[i], err
	}
	return fantasy.NewTextResponse(`{"ok":true}`), err
}

func guardCtx(t *testing.T) context.Context {
	ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
	recordDesktopWindows(ctx, "windows", fantasy.NewTextResponse(`{"result":[{"window_id":"11"}]}`))
	return ctx
}

func computerCall(input string) fantasy.ToolCall {
	return fantasy.ToolCall{ID: "c", Name: ComputerToolName, Input: input}
}

func failure(text string) fantasy.ToolResponse { return fantasy.NewTextErrorResponse(text) }

func TestGuardBlocksIdenticalFailedCallUntilStateChanges(t *testing.T) {
	t.Parallel()
	inner := &scriptedTool{name: ComputerToolName, replies: []fantasy.ToolResponse{
		failure("wrong_window: focus the intended window and inspect again before input"),
	}}
	guarded := WithDesktopGuard(inner)
	ctx := guardCtx(t)
	key := computerCall(`{"action":"hotkey","key":"l","modifiers":"ctrl","automation":{"window_id":"11"}}`)
	first, err := guarded.Run(ctx, key)
	require.NoError(t, err)
	require.Contains(t, first.Content, "wrong_window")
	require.Contains(t, first.Metadata, `"class":"target_state"`)

	// Same call, same state: refused without reaching the tool.
	second, err := guarded.Run(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "repeat_blocked", contractOf(t, second).Code)
	require.EqualValues(t, 1, inner.calls)

	// Reordered JSON keys are the same call.
	reordered := computerCall(`{"automation":{"window_id":"11"},"modifiers":"ctrl","key":"l","action":"hotkey"}`)
	third, _ := guarded.Run(ctx, reordered)
	require.Equal(t, "repeat_blocked", contractOf(t, third).Code)

	// Observation alone does not change state.
	_, _ = guarded.Run(ctx, computerCall(`{"action":"windows"}`))
	again, _ := guarded.Run(ctx, key)
	require.Equal(t, "repeat_blocked", contractOf(t, again).Code)

	// A successful mutation (e.g. focus) changes the state; the retry may run.
	_, _ = guarded.Run(ctx, computerCall(`{"action":"focus","automation":{"window_id":"11"}}`))
	after, _ := guarded.Run(ctx, key)
	require.False(t, after.IsError, after.Content)
}

func TestGuardNeverReplaysArgumentErrors(t *testing.T) {
	t.Parallel()
	inner := &scriptedTool{name: ComputerToolName, replies: []fantasy.ToolResponse{failure("invalid_role: bad")}}
	guarded := WithDesktopGuard(inner)
	ctx := guardCtx(t)
	call := computerCall(`{"action":"find","automation":{"window_id":"11","role":"x"}}`)
	_, _ = guarded.Run(ctx, call)
	_, _ = guarded.Run(ctx, computerCall(`{"action":"focus","automation":{"window_id":"11"}}`)) // State change does not matter.
	blocked, _ := guarded.Run(ctx, call)
	require.Equal(t, "repeat_blocked", contractOf(t, blocked).Code)
	require.False(t, contractOf(t, blocked).FreshObservation)
	require.EqualValues(t, 2, inner.calls)
}

func TestGuardDoesNotReplayUncertainMutationUntilStateIsRead(t *testing.T) {
	t.Parallel()
	inner := &scriptedTool{name: ComputerToolName, errs: []error{context.DeadlineExceeded}}
	guarded := WithDesktopGuard(inner)
	ctx := guardCtx(t)
	typeCall := computerCall(`{"action":"type","text":"1234","automation":{"window_id":"11"}}`)
	_, err := guarded.Run(ctx, typeCall)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	blocked, err := guarded.Run(ctx, typeCall)
	require.NoError(t, err)
	contract := contractOf(t, blocked)
	require.Equal(t, "repeat_blocked", contract.Code)
	require.Contains(t, contract.Message, "may or may not have taken effect")
	require.True(t, contract.FreshObservation)
	require.EqualValues(t, 1, inner.calls)

	// Neither another mutation nor an inspect proves the original effect.
	_, _ = guarded.Run(ctx, computerCall(`{"action":"key","key":"tab","automation":{"window_id":"11"}}`))
	stillBlocked, _ := guarded.Run(ctx, typeCall)
	require.Equal(t, "repeat_blocked", contractOf(t, stillBlocked).Code)
	_, _ = guarded.Run(ctx, computerCall(`{"action":"inspect","automation":{"window_id":"11"}}`))
	blockedAgain, _ := guarded.Run(ctx, typeCall)
	require.Equal(t, "repeat_blocked", contractOf(t, blockedAgain).Code,
		"even inspecting the same window does not prove whether typing took effect")
}

func TestGuardUnrelatedReadsCannotAuthorizeReplay(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"screen_size", "status", "cursor_position", "windows"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			inner := &scriptedTool{name: ComputerToolName, errs: []error{context.DeadlineExceeded}}
			tool := WithDesktopGuard(inner)
			ctx := guardCtx(t)
			call := computerCall(`{"action":"type","text":"1234","automation":{"window_id":"11"}}`)
			_, _ = tool.Run(ctx, call)
			_, _ = tool.Run(ctx, computerCall(`{"action":"`+action+`"}`))
			resp, _ := tool.Run(ctx, call)
			require.True(t, resp.IsError, "unrelated read permitted duplicate input")
			require.EqualValues(t, 2, inner.calls)
		})
	}
}

func TestGuardRequiresObservedWindowBeforeMutation(t *testing.T) {
	t.Parallel()
	inner := &scriptedTool{name: ComputerToolName}
	tool := WithDesktopGuard(inner)
	ctx := guardCtx(t)
	call := computerCall(`{"action":"focus","automation":{"window_id":"987654"}}`)
	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	require.True(t, resp.IsError, "a guessed numeric handle reached the native tool")
	require.Contains(t, resp.Content, "unobserved_target")
	require.Zero(t, inner.calls)
}

func TestGUIOnlyTaskRejectsShellAndFileEditsBeforeExecution(t *testing.T) {
	t.Parallel()
	for _, name := range []string{BashToolName, WriteToolName, EditToolName, MultiEditToolName} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inner := &scriptedTool{name: name}
			ctx := WithDesktopTaskScope(guardCtx(t), "Bu işlemi computer use ile masaüstü uygulamasından yap.")
			resp, err := WithDesktopGuard(inner).Run(ctx, fantasy.ToolCall{Name: name, Input: `{"command":"python -c 'print(123)'"}`})
			require.NoError(t, err)
			require.True(t, resp.IsError)
			require.Contains(t, resp.Content, "task_scope_violation")
			require.Zero(t, inner.calls, "scope must be enforced before executing any command")
		})
	}
}

func TestGUIOnlyScopeSurvivesSuccessfulReadAndNestedPipeline(t *testing.T) {
	t.Parallel()
	ctx := WithDesktopTaskScope(guardCtx(t), "Use the desktop application to save the document.")
	desktop := WithDesktopGuard(&scriptedTool{name: ComputerToolName})
	_, _ = desktop.Run(ctx, computerCall(`{"action":"screen_size"}`))
	shellInner := &scriptedTool{name: BashToolName}
	shell := WithDesktopGuard(shellInner)
	pipeline := WithDesktopGuard(NewToolPipeline(func(c context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return shell.Run(c, call)
	}))
	resp, err := pipeline.Run(ctx, fantasy.ToolCall{Name: "tool_pipeline", Input: `{"steps":[{"id":"shell","tool":"bash","arguments":{"command":"mkdir x"}}]}`})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "task_scope_violation")
	require.Zero(t, shellInner.calls)
}

func TestGuardObservedTargetsExpireAndEnumerationRemovesClosedWindows(t *testing.T) {
	t.Parallel()
	ctx := guardCtx(t)
	inner := &scriptedTool{name: ComputerToolName}
	tool := WithDesktopGuard(inner)
	call := computerCall(`{"action":"focus","automation":{"window_id":"11"}}`)
	resp, _ := tool.Run(ctx, call)
	require.False(t, resp.IsError, resp.Content)
	recordDesktopWindows(ctx, "windows", fantasy.NewTextResponse(`{"result":[]}`))
	resp, _ = tool.Run(ctx, call)
	require.Contains(t, resp.Content, "unobserved_target")
	recordDesktopWindows(ctx, "windows", fantasy.NewTextResponse(`{"result":[{"window_id":"11"}]}`))
	state := desktopGuardFor(GetSessionFromContext(ctx))
	state.mu.Lock()
	state.windows["11"] = time.Now().Add(-6 * time.Minute)
	state.mu.Unlock()
	resp, _ = tool.Run(ctx, call)
	require.Contains(t, resp.Content, "unobserved_target")
	require.EqualValues(t, 1, inner.calls)
}

func TestGuardEquivalentProviderEncodingsCannotBypassReplayBlock(t *testing.T) {
	t.Parallel()
	ctx := guardCtx(t)
	inner := &scriptedTool{name: ComputerToolName, errs: []error{context.DeadlineExceeded}}
	tool := WithDesktopGuard(inner)
	_, err := tool.Run(ctx, computerCall(`{"action":"hotkey","key":"l","modifiers":"ctrl","automation":{"window_id":"11"}}`))
	require.Error(t, err)
	resp, _ := tool.Run(ctx, computerCall(`{"action":"hotkey","automation":{"window_id":"11","key":"l","modifiers":"ctrl"}}`))
	require.Contains(t, resp.Content, "repeat_blocked")
	require.EqualValues(t, 1, inner.calls)
}
