package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopObserveDispatchesOnlyRead(t *testing.T) {
	t.Parallel()
	calls := 0
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "observe", WindowID: "11", MaxElements: 40}, fantasy.ToolCall{ID: "observe"}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		require.Equal(t, ComputerToolName, call.Name)
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		require.Equal(t, "observe", p.Action)
		require.Equal(t, "semantic", p.Observation)
		require.Equal(t, "11", p.Automation.WindowID)
		require.Equal(t, 40, p.Automation.MaxElements)
		return fantasy.NewTextResponse(`{"window_id":"11","elements":[]}`), nil
	})
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Equal(t, 1, calls)
}

func TestDesktopObserveRejectsMutationsBeforeDispatch(t *testing.T) {
	t.Parallel()
	for _, p := range []DesktopWorkflowParams{
		{Mode: "observe"},
		{Mode: "observe", WindowID: "11", Application: "Notepad"},
		{Mode: "observe", WindowID: "11", Input: ComputerParams{Action: "type", Text: "unsafe"}},
		{Mode: "observe", WindowID: "11", MaxElements: 501},
		{Mode: "observe", WindowID: "11", Observation: "invalid"},
	} {
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			t.Fatal("Invalid observe dispatched a child")
			return fantasy.ToolResponse{}, nil
		})
		require.NoError(t, err)
		require.True(t, r.IsError)
	}
}
