package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// A desktop flow halted by a hook on one of its children still renders the
// hook indicator in the chat: the flow adds its ledger beside the child's
// metadata instead of replacing it.
func TestFlowHaltedByHookRendersHookIndicator(t *testing.T) {
	t.Parallel()
	hookMeta, err := json.Marshal(map[string]any{"hook": hooks.HookMetadata{HookCount: 1, Decision: "deny", Halt: true, Reason: "Approve first", Hooks: []hooks.HookInfo{{Name: "submit-guard", Decision: "deny", Halt: true, Reason: "Approve first"}}}})
	require.NoError(t, err)
	invoke := func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p tools.ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		if p.Action == "windows" {
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Main A","process_id":17,"process_name":"Notepad.exe","class_name":"Notepad","foreground":true}]}`), nil
		}
		r := fantasy.NewTextErrorResponse("Turn halted by hook. Reason: Approve first")
		r.StopTurn, r.Metadata = true, string(hookMeta)
		return tools.WithComputerNotDispatched(r, call), nil
	}
	plan := `{"desktop":{"mode":"flow","flow":{"nodes":[
 {"id":"a","kind":"resolve","title":"Main A","next":"submit"},
 {"id":"submit","kind":"operation","window_ref":"a","input":{"action":"key","key":"enter"},"checkpoint":{"name":"Field","role":"ControlType.Edit","condition":"value","expected":"submitted"}}]}}}`
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "chat-flow-hook")
	resp, err := tools.NewToolPipeline(invoke).Run(ctx, fantasy.ToolCall{ID: "flow", Name: tools.ToolPipelineName, Input: plan})
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	require.Contains(t, resp.Metadata, `"flow":`)

	sty := styles.AtlasPantera()
	line := ansi.Strip(toolOutputHookIndicator(&sty, resp.Metadata, 120))
	require.True(t, strings.Contains(line, "submit-guard"), line)
}
