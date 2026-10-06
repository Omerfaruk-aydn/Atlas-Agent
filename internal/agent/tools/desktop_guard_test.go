package tools

import (
	"context"

	"sync/atomic"
	"testing"

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
