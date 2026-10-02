package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codegraph"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestProjectGraphResolvedCallsAndFreshness(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\ngo 1.26.6\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Run() {}\nfunc main() { Run() }\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "other"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "other", "other.go"), []byte("package other\nfunc Run() {}\n"), 0o644))
	tool := NewProjectMapTool(root, engineering.NewStore(t.TempDir()))
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "graph", Input: `{"action":"refresh_graph"}`})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	var graph struct {
		Nodes []struct{ ID, Path, Symbol string }
		Edges []struct{ From, To, Kind string }
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &graph))
	var from, to string
	for _, node := range graph.Nodes {
		if node.Path == "main.go" && node.Symbol == "main" {
			from = node.ID
		}
		if node.Path == "main.go" && node.Symbol == "Run" {
			to = node.ID
		}
	}
	require.NotEmpty(t, from)
	require.NotEmpty(t, to)
	count := 0
	for _, edge := range graph.Edges {
		if edge.Kind == "calls" && edge.From == from {
			require.Equal(t, to, edge.To)
			count++
		}
	}
	require.Equal(t, 1, count)
	resp, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "symbols", Input: `{"action":"symbols","query":"Run"}`})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Changed() {}\n"), 0o644))
	resp, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "stale", Input: `{"action":"symbols"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError, "Changed source cannot reuse a graph as current")
}

type fixtureLSPGraph struct{ root string }

func (f fixtureLSPGraph) GraphForFile(ctx context.Context, path string) (codegraph.CodeGraph, error) {
	data, err := os.ReadFile(path)
	return codegraph.CodeGraph{Root: f.root, Nodes: []codegraph.Node{{ID: "symbol", Path: "page.ts", Symbol: "Page", FileHash: engineering.Hash(string(data))}}, Partial: true}, err
}

func TestProjectGraphScopedLSP(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "page.ts"), []byte("export function Page() {}"), 0o644))
	tool := NewProjectMapTool(root, engineering.NewStore(t.TempDir()), fixtureLSPGraph{root})
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "lsp", Input: `{"action":"refresh_graph","file_path":"page.ts"}`})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "Page")
	resp, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "escape", Input: `{"action":"refresh_graph","file_path":"../outside.ts"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
}
