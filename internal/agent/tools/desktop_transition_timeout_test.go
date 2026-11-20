package tools

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// When the transition wait runs out while a window listing is still in
// flight, the run ends in condition_timeout, exactly as when it runs out
// between polls, instead of a bare deadline error.
func TestTransitionTimeoutDuringWindowListing(t *testing.T) {
	t.Parallel()
	var listings atomic.Int32
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		_ = json.Unmarshal([]byte(call.Input), &p)
		if p.Action == "windows" && listings.Add(1) > 1 {
			// Every poll after the baseline outlives the wait.
			<-ctx.Done()
			return fantasy.ToolResponse{}, ctx.Err()
		}
		return calculatorReplies(call), nil
	}
	input := `{"desktop":{"mode":"transition","input":{"action":"hotkey","modifiers":"ctrl","key":"s","automation":{"window_id":"11"}},"transition":{"wait_ms":200,"expected_title":"Save As"}}}`
	resp, err := NewToolPipeline(invoke).Run(t.Context(), fantasy.ToolCall{ID: "call", Name: ToolPipelineName, Input: input})
	require.NoError(t, err)
	require.True(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "condition_timeout")
	require.GreaterOrEqual(t, listings.Load(), int32(2), "the wait ran out during a polling listing")
}
