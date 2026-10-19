package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codemap"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestCodeMapToolAnswersEachAction(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for rel, content := range map[string]string{
		"lib.py":  "def area(r):\n    return r * r\n",
		"main.py": "from .lib import area\n\ndef run():\n    area(2)\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644))
	}
	tool := NewCodeMapTool(root, codemap.NewBuilder(t.TempDir()))
	run := func(params CodeMapParams) fantasy.ToolResponse {
		input, err := json.Marshal(params)
		require.NoError(t, err)
		resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "call-1", Name: CodeMapToolName, Input: string(input)})
		require.NoError(t, err)
		return resp
	}

	overview := run(CodeMapParams{})
	require.False(t, overview.IsError)
	require.Contains(t, overview.Content, "2 files (python 2)")

	require.Contains(t, run(CodeMapParams{Action: "find", Query: "area"}).Content, "function area  lib.py:1")
	require.Contains(t, run(CodeMapParams{Action: "explain", Query: "area"}).Content, "main.py:4 in function run [call, resolved]")
	require.Contains(t, run(CodeMapParams{Action: "callers", Query: "area"}).Content, "main.py:4 in function run -> area")
	require.Contains(t, run(CodeMapParams{Action: "path", Query: "run", To: "area"}).Content, "reaches area in 1 step")

	missing := run(CodeMapParams{Action: "explain"})
	require.True(t, missing.IsError)
	require.Contains(t, missing.Content, "needs a query")
	require.True(t, run(CodeMapParams{Action: "path", Query: "run"}).IsError)
	require.True(t, run(CodeMapParams{Action: "draw", Query: "run"}).IsError)
}
