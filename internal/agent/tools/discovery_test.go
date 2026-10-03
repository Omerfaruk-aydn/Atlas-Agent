package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestToolSearchUsesAuthorizedPaletteWithoutExecution(t *testing.T) {
	t.Parallel()
	fixture := fantasy.NewAgentTool("fixture_test", "Run fixture tests", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		t.Fatal("discovery must not execute tools")
		return fantasy.ToolResponse{}, nil
	})
	tool := NewToolSearchTool(func(context.Context) []fantasy.AgentTool { return []fantasy.AgentTool{fixture} })
	response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: `{"query":"fixture test"}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	var result struct {
		Tools []fantasy.ToolInfo `json:"tools"`
		Total int                `json:"total"`
	}
	require.NoError(t, json.Unmarshal([]byte(response.Content), &result))
	require.Equal(t, 1, result.Total)
	require.Equal(t, "fixture_test", result.Tools[0].Name)
	response, err = tool.Run(t.Context(), fantasy.ToolCall{Input: `{"query":"disabled"}`})
	require.NoError(t, err)
	require.Contains(t, response.Content, `"total":0`)
}

func TestCodeQueryScopesAndPreservesGuardedDispatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "app.ts"), []byte("export function Run() {}"), 0o644))
	calls := 0
	tool := NewCodeQueryTool(root, func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		require.Equal(t, DefinitionToolName, call.Name)
		var p DefinitionParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		require.Equal(t, "Run", p.Symbol)
		require.Equal(t, filepath.Join(root, "app.ts"), p.Path)
		return fantasy.NewTextErrorResponse("required tool is disabled"), nil
	})
	response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: `{"action":"definition","file_path":"app.ts","symbol":"Run"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Equal(t, 1, calls)
	response, err = tool.Run(t.Context(), fantasy.ToolCall{Input: `{"action":"symbols","file_path":"../outside.ts"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Equal(t, 1, calls)
}
