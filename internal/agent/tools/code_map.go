package tools

import (
	"context"
	_ "embed"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codemap"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// CodeMapToolName is the name of the code map tool.
const CodeMapToolName = "code_map"

//go:embed code_map.md
var codeMapDescription string

// CodeMapParams are the code map tool's parameters.
type CodeMapParams struct {
	Action string `json:"action" description:"overview: the project's languages, subsystems, entry points and most depended-on files with their main definitions. find: definitions and files whose name matches query. explain: one definition or file and how it connects (who uses it, what it uses, imports, files changed together). callers: what changing query would affect, level by level. path: how query connects to to." enum:"overview,find,explain,callers,path"`
	Query  string `json:"query,omitempty" description:"A definition name (solveKepler), Container.Name (Orbit.step), path:Name (src/app.ts:main) or a file path. Required except for overview."`
	To     string `json:"to,omitempty" description:"For path: the definition or file to reach."`
	Depth  int    `json:"depth,omitempty" description:"For callers: how many levels of users to follow, 1-5, default 2."`
	Budget int    `json:"budget,omitempty" description:"For overview: about how many tokens to spend, default 2000, at most 8000."`
}

// NewCodeMapTool maps the code under workingDir. builder keeps the map
// between calls, so a query after an edit re-reads only the edited files.
func NewCodeMapTool(workingDir string, builder *codemap.Builder) fantasy.AgentTool {
	return fantasy.NewParallelAgentTool(CodeMapToolName, codeMapDescription, func(ctx context.Context, p CodeMapParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
		action := strings.ToLower(strings.TrimSpace(p.Action))
		if action == "" {
			action = "overview"
		}
		query := strings.TrimSpace(p.Query)
		if action != "overview" && query == "" {
			return fantasy.NewTextErrorResponse(action + " needs a query: a definition name, Container.Name, path:Name or a file path"), nil
		}
		if action == "path" && strings.TrimSpace(p.To) == "" {
			return fantasy.NewTextErrorResponse("path needs to: the definition or file to reach"), nil
		}
		m, err := builder.Map(ctx, workingDir)
		if err != nil {
			return fantasy.NewTextErrorResponse("cannot map the project: " + err.Error()), nil
		}
		var out string
		switch action {
		case "overview":
			out = m.Overview(p.Budget)
		case "find":
			out = m.Find(query)
		case "explain":
			out = m.Explain(ctx, query)
		case "callers":
			out = m.Callers(query, p.Depth)
		case "path":
			out = m.Path(query, strings.TrimSpace(p.To))
		default:
			return fantasy.NewTextErrorResponse("unknown action " + p.Action + "; use overview, find, explain, callers or path"), nil
		}
		return fantasy.NewTextResponse(out), nil
	})
}
