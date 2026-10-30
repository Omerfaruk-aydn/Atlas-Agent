package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/factstore"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/projectmodel"
	"github.com/stretchr/testify/require"
)

func projectModelTestTool(t *testing.T) fantasy.AgentTool {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":              "module example.com/app\n\ngo 1.22\n",
		"main.go":             "package main\n\nimport \"example.com/app/lib\"\n\nfunc main() { lib.Run() }\n",
		"lib/lib.go":          "package lib\n\nfunc Run() {}\n",
		"lib/lib_test.go":     "package lib\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) { Run() }\n",
		"README.md":           "# App\n",
		"docs/adr/0001-go.md": "# Use Go\n",
	}
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	dataDir := filepath.Join(root, ".atlas")
	svc := projectmodel.NewService(root, dataDir, nil, factstore.New(filepath.Join(dataDir, "facts.jsonl")))
	return NewProjectModelTool(svc, permission.NewPermissionService(t.TempDir(), true, nil))
}

func runProjectModel(t *testing.T, tool fantasy.AgentTool, input string) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "call", Input: input})
	require.NoError(t, err)
	return resp
}

func TestProjectModelToolActions(t *testing.T) {
	t.Parallel()
	tool := projectModelTestTool(t)

	impact := runProjectModel(t, tool, `{"action":"impact","files":["lib/lib.go"]}`)
	require.False(t, impact.IsError, impact.Content)
	require.Contains(t, impact.Content, "main.go")
	require.Contains(t, impact.Content, "go test ./lib")

	tests := runProjectModel(t, tool, `{"action":"tests","files":["lib/lib.go"]}`)
	require.Contains(t, tests.Content, "lib/lib_test.go (direct)")

	query := runProjectModel(t, tool, `{"action":"query","query":"lib"}`)
	require.Contains(t, query.Content, "Module lib")

	summary := runProjectModel(t, tool, `{"action":"summary"}`)
	require.Contains(t, summary.Content, "main.go")

	decide := runProjectModel(t, tool, `{"action":"decide","text":"lib stays dependency free","rationale":"imported everywhere","files":["lib"]}`)
	require.False(t, decide.IsError, decide.Content)
	again := runProjectModel(t, tool, `{"action":"impact","files":["lib/lib.go"]}`)
	require.Contains(t, again.Content, "lib stays dependency free")
	decisions := runProjectModel(t, tool, `{"action":"decisions","query":"dependency"}`)
	require.Contains(t, decisions.Content, "imported everywhere")

	refresh := runProjectModel(t, tool, `{"action":"refresh"}`)
	require.Contains(t, refresh.Content, "up to date")
}

func TestProjectModelToolRejectsBadInput(t *testing.T) {
	t.Parallel()
	tool := projectModelTestTool(t)
	for _, input := range []string{
		`{"action":"explode"}`,
		`{"action":"query"}`,
		`{"action":"tests"}`,
		`{"action":"decide"}`,
	} {
		resp := runProjectModel(t, tool, input)
		require.True(t, resp.IsError, input)
	}
}
