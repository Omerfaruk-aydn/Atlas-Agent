package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type ProjectMapParams struct {
	Action   string `json:"action,omitempty" description:"refresh, query, refresh_graph, symbols or impact. Graph queries validate current source fingerprints."`
	Query    string `json:"query,omitempty" description:"Filter paths, package names or imports."`
	Offset   int    `json:"offset,omitempty" description:"Page offset; at most 50 files are returned."`
	Cursor   string `json:"cursor,omitempty" description:"Source-bound cursor returned by symbols."`
	NodeID   string `json:"node_id,omitempty" description:"Graph node identity for impact."`
	Depth    int    `json:"depth,omitempty" description:"Impact depth 1-5, default 2."`
	FilePath string `json:"file_path,omitempty" description:"Optional literal relative source file for scoped LSP graph analysis."`
}

func NewProjectMapTool(root string, store *engineering.Store, providers ...LSPGraphProvider) fantasy.AgentTool {
	return fantasy.NewAgentTool("project_map", "Inspect a persistent bounded project map with entry points, test files and Go imports. Refresh after changes. Query results are snapshots, not live facts; oversized/generated/secret files are skipped and truncation is disclosed.", func(ctx context.Context, p ProjectMapParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if p.Offset < 0 || len(p.Query) > 256 {
			return fantasy.NewTextErrorResponse("invalid query or offset"), nil
		}
		if p.Action == "refresh_graph" || p.Action == "symbols" || p.Action == "impact" {
			return projectGraph(ctx, root, store, p, providers...)
		}
		var m engineering.ProjectMap
		var err error
		switch p.Action {
		case "refresh":
			m, err = store.RefreshProject(ctx, root)
		case "", "query":
			m, err = store.Project(ctx, root)
		default:
			return fantasy.NewTextErrorResponse("use query or refresh"), nil
		}
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		var files []engineering.ProjectFile
		for _, f := range m.Files {
			if strings.Contains(strings.ToLower(f.Path+" "+f.Package+" "+strings.Join(f.Imports, " ")), strings.ToLower(p.Query)) {
				files = append(files, f)
			}
		}
		total := len(files)
		start := min(p.Offset, total)
		files = files[start:min(start+50, total)]
		data, _ := json.Marshal(map[string]any{"files": files, "total": total, "offset": start, "map_truncated": m.Truncated, "changed": m.Changed, "removed": m.Removed})
		return fantasy.NewTextResponse(string(data)), nil
	})
}
