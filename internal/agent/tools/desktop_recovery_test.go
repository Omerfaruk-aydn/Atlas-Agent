package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopRecoveryObservesWithoutReplaying(t *testing.T) {
	var actions []string
	invoke := desktopRecoveryInvoke(t.Context(), fantasy.ToolCall{ID: "parent"}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		actions = append(actions, p.Action)
		switch p.Action {
		case "invoke":
			return fantasy.NewTextErrorResponse("accessibility_unavailable: unsupported pattern"), nil
		case "windows":
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","foreground":true}]}`), nil
		case "observe":
			return fantasy.NewTextResponse(`{"window_id":"11","elements":[]}`), nil
		}
		return fantasy.ToolResponse{}, nil
	})
	r, err := invoke(t.Context(), fantasy.ToolCall{Name: ComputerToolName, Input: `{"action":"invoke","automation":{"window_id":"11"}}`})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, []string{"invoke", "windows", "observe"}, actions)
	require.Contains(t, r.Metadata, "input_replayed")
	require.Contains(t, r.Metadata, "fresh_observation")
	require.Contains(t, r.Content, "fresh_observation", "Recovery evidence must be visible in the model tool message")
}

func TestDesktopRecoveryStopsOnDenialAndHandoff(t *testing.T) {
	for _, stop := range []bool{false, true} {
		calls := 0
		invoke := desktopRecoveryInvoke(t.Context(), fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			calls++
			r := fantasy.NewTextErrorResponse("permission_denied: user denied")
			r.StopTurn = stop
			return r, nil
		})
		_, err := invoke(t.Context(), fantasy.ToolCall{Name: ComputerToolName, Input: `{"action":"key"}`})
		require.NoError(t, err)
		require.Equal(t, 1, calls)
	}
}
