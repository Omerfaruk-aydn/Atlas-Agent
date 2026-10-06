package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopSequenceCheckpointResultUsesMultipleWindowsWithoutImages(t *testing.T) {
	var p DesktopWorkflowParams
	require.NoError(t, json.Unmarshal([]byte(`{"mode":"sequence","result":"checkpoints","steps":[{"focus_window":true,"input":{"action":"type","text":"hello","automation":{"window_id":"11"}},"checkpoint":{"window_id":"11","name":"Field","condition":"value","expected":"hello"}},{"focus_window":true,"input":{"action":"key","key":"enter","automation":{"window_id":"22"}},"checkpoint":{"window_id":"22","element_id":"result","condition":"text","expected":"done"}}]}`), &p))
	var actions []string
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		actions = append(actions, input.Action)
		switch input.Action {
		case "focus":
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		case "assert":
			data, _ := json.Marshal(map[string]any{"window_id": input.Automation.WindowID, "passed": true, "actual": input.Automation.Expected})
			return fantasy.NewTextResponse(string(data)), nil
		case "windows":
			return fantasy.NewTextResponse(`{"result":[{"window_id":"22","foreground":true}]}`), nil
		default:
			return fantasy.NewTextResponse(`{}`), nil
		}
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, []string{"focus", "type", "assert", "focus", "key", "assert", "windows"}, actions)
	require.Equal(t, "text", r.Type)
	require.Contains(t, r.Content, `"completed_steps":2`)
	require.Contains(t, r.Content, `"actual":"hello"`)
	require.NotContains(t, r.Content, "snapshot_id")
}
