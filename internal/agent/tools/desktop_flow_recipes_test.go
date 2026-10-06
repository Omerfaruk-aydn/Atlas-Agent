package tools

import (
	"context"
	"encoding/json"

	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopFlowPrepareWriteAndCloseInOneCall(t *testing.T) {
	p := desktopFlowTestPlan()
	p.Flow.Nodes[0].Kind = "prepare"
	p.Flow.Nodes[1].Next = "close"
	p.Flow.Nodes = append(p.Flow.Nodes, DesktopFlowNode{ID: "close", Kind: "close", WindowRef: "app"})
	value, mutations, closed := "empty", 0, false
	base := desktopFlowFixture(t, &value, &mutations)
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		switch input.Action {
		case "observe":
			return fantasy.NewTextResponse(`{"window_id":"11","elements":[{"role":"ControlType.Edit","name":"Field"}]}`), nil
		case "hotkey":
			require.Equal(t, "11", input.Automation.WindowID)
			require.Equal(t, "alt", input.Modifiers)
			require.Equal(t, "f4", input.Key)
			closed = true
			return fantasy.NewTextResponse("Closed"), nil
		case "windows":
			if closed {
				return fantasy.NewTextResponse(`{"result":[]}`), nil
			}
		}
		return base(ctx, c)
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.True(t, closed)
	require.Equal(t, 1, mutations)
	require.Contains(t, r.Content, `"absence_verified":true`)
	require.Contains(t, r.Content, `"kind":"prepare"`)
}
