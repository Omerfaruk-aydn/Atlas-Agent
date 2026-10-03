package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

const ToolSearchToolName = "tool_search"

//go:embed tool_search.md
var toolSearchDescription string

type ToolSearchParams struct {
	Query string `json:"query" description:"Short words matching the enabled tool name and description."`
	Limit int    `json:"limit,omitempty" description:"Maximum results, 1-20; default 8."`
}

// NewToolSearchTool receives the current authorized palette, never all tools.
func NewToolSearchTool(palette func(context.Context) []fantasy.AgentTool, activate ...func(context.Context, []fantasy.ToolInfo) error) fantasy.AgentTool {
	return fantasy.NewAgentTool(ToolSearchToolName, toolSearchDescription, func(ctx context.Context, p ToolSearchParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
		words := strings.Fields(strings.ToLower(p.Query))
		if len(words) == 0 || len(p.Query) > 256 || p.Limit < 0 || p.Limit > 20 {
			return fantasy.NewTextErrorResponse("provide a nonempty query of at most 256 bytes and limit 1-20"), nil
		}
		if p.Limit == 0 {
			p.Limit = 8
		}
		matches := make([]fantasy.ToolInfo, 0)
		for _, tool := range palette(ctx) {
			if err := ctx.Err(); err != nil {
				return fantasy.ToolResponse{}, err
			}
			info := tool.Info()
			if info.Name == ToolSearchToolName {
				continue
			}
			haystack := strings.ToLower(info.Name + " " + info.Description)
			if !slices.ContainsFunc(words, func(word string) bool { return !strings.Contains(haystack, word) }) {
				matches = append(matches, info)
			}
		}
		slices.SortFunc(matches, func(a, b fantasy.ToolInfo) int { return strings.Compare(a.Name, b.Name) })
		total := len(matches)
		if len(activate) > 0 && activate[0] != nil {
			if err := activate[0](ctx, matches[:min(total, p.Limit)]); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
		}
		data, err := json.Marshal(map[string]any{"tools": matches[:min(total, p.Limit)], "total": total, "truncated": total > p.Limit})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return fantasy.NewTextResponse(string(data)), nil
	})
}
