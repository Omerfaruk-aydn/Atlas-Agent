package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/stretchr/testify/require"
)

func TestDesktopFileCheckRunsChildHookAndKeepsHalt(t *testing.T) {
	t.Parallel()
	inner := &fakeTool{name: tools.ComputerToolName, resp: fantasy.NewTextResponse("unexpected file read")}
	hooked := newHookedTool(inner, newRunner(t, `read -r payload; case "$payload" in *'"action":"file_check"'*) echo '{"decision":"deny","halt":true,"reason":"Approve this file read"}' ;; esac`), nil)
	input, err := json.Marshal(tools.PipelineParams{Desktop: &tools.DesktopWorkflowParams{Mode: "flow", Flow: &tools.DesktopFlowParams{Nodes: []tools.DesktopFlowNode{
		{ID: "saved", Kind: "file_check", File: &tools.DesktopFileCheck{Path: filepath.Join(t.TempDir(), "secret.txt"), Condition: "content", Expected: "secret"}},
	}}}})
	require.NoError(t, err)
	r, err := tools.NewToolPipeline(hooked.Run).Run(t.Context(), fantasy.ToolCall{ID: "file-flow", Name: tools.ToolPipelineName, Input: string(input)})
	require.NoError(t, err)
	require.False(t, inner.called, "the hook must block before any file read")
	require.True(t, r.StopTurn)
	var meta struct {
		Hook *hooks.HookMetadata `json:"hook"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Metadata), &meta))
	require.NotNil(t, meta.Hook)
	require.True(t, meta.Hook.Halt)
	require.Equal(t, "Approve this file read", meta.Hook.Reason)
	require.Contains(t, r.Metadata, `"state":"blocked"`)
}
