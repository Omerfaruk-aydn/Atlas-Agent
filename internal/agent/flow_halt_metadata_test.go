package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/stretchr/testify/require"
)

// fieldComputer is a scripted computer tool with one window holding one
// edit control. A key press submits the field.
type fieldComputer struct {
	mu    sync.Mutex
	value string
	keys  int
}

func (c *fieldComputer) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: tools.ComputerToolName}
}
func (c *fieldComputer) ProviderOptions() fantasy.ProviderOptions     { return nil }
func (c *fieldComputer) SetProviderOptions(_ fantasy.ProviderOptions) {}

func (c *fieldComputer) Run(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var p tools.ComputerParams
	if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch p.Action {
	case "windows":
		return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Main A","process_id":17,"process_name":"Notepad.exe","class_name":"Notepad","foreground":true}]}`), nil
	case "assert":
		data, _ := json.Marshal(map[string]any{"window_id": "11", "actual": c.value, "passed": c.value == p.Automation.Expected})
		return fantasy.NewTextResponse(string(data)), nil
	case "key":
		c.keys++
		c.value = "submitted"
		return fantasy.NewTextResponse("Sent"), nil
	}
	return fantasy.NewTextErrorResponse("unexpected action " + p.Action), nil
}

// A PreToolUse hook that halts a desktop flow's key press is reported with
// its hook metadata where the TUI reads it, with adapter-owned dispatch:none
// evidence and with the flow ledger beside it. Nothing is sent, and after
// the hook allows the call the same durable run resumes and submits once.
func TestFlowKeepsHookHaltMetadataAndResumesAfterApproval(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "flow-hook-halt")
	store := engineering.NewStore(t.TempDir())
	computer := &fieldComputer{}
	plan := `{"desktop":{"mode":"flow","flow":{"run_id":"hook-halt","nodes":[
 {"id":"a","kind":"resolve","title":"Main A","next":"submit"},
 {"id":"submit","kind":"operation","window_ref":"a","input":{"action":"key","key":"enter"},"checkpoint":{"name":"Field","role":"ControlType.Edit","condition":"value","expected":"submitted"}}]}}}`

	halting := newHookedTool(computer, newRunner(t, `read -r payload; case "$payload" in *'"action":"key"'*) echo '{"decision":"deny","halt":true,"reason":"Approve the submit first"}' ;; esac`), nil)
	resp, err := tools.NewToolPipeline(halting.Run, store).Run(ctx, fantasy.ToolCall{ID: "first", Name: tools.ToolPipelineName, Input: plan})
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	require.True(t, strings.HasPrefix(resp.Content, "Turn halted by hook. Reason: Approve the submit first"), resp.Content)

	var meta struct {
		Hook *hooks.HookMetadata `json:"hook"`
		Flow struct {
			Nodes []map[string]string `json:"nodes"`
		} `json:"flow"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta), resp.Metadata)
	require.NotNil(t, meta.Hook, "the hook indicator still finds its metadata")
	require.True(t, meta.Hook.Halt)
	require.Equal(t, "Approve the submit first", meta.Hook.Reason)
	require.Equal(t, map[string]string{"id": "submit", "kind": "operation", "state": "blocked"}, meta.Flow.Nodes[1])
	native, ok := tools.ResponseOutcome(resp)
	require.True(t, ok)
	require.Equal(t, "none", native.Dispatch)
	require.Zero(t, computer.keys)

	allowing := newHookedTool(computer, newRunner(t, `exit 0`), nil)
	resp, err = tools.NewToolPipeline(allowing.Run, store).Run(ctx, fantasy.ToolCall{ID: "second", Name: tools.ToolPipelineName, Input: strings.Replace(plan, `"run_id":"hook-halt"`, `"run_id":"hook-halt","resume":true`, 1)})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, 1, computer.keys)
}

// The outcome guard classifies a hook-refused computer call as not sent.
func TestHookRefusedComputerCallIsRecordedAsNotSent(t *testing.T) {
	t.Parallel()
	inner := &fakeTool{name: tools.ComputerToolName, resp: fantasy.NewTextResponse("unexpected")}
	tool := newHookedTool(inner, newRunner(t, `echo '{"decision":"deny","reason":"no"}'`), nil)
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "c", Name: tools.ComputerToolName, Input: `{"action":"key","key":"enter","automation":{"window_id":"11"}}`})
	require.NoError(t, err)
	require.False(t, inner.called)
	result := outcomeResult(tools.ComputerToolName, resp, nil, true, false)
	require.True(t, result.DispatchKnown)
	require.False(t, result.Sent)
	require.Contains(t, resp.Metadata, `"hook":`)
}
