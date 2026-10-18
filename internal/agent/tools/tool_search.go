package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
)

const ToolSearchToolName = "tool_search"

const (
	toolSearchDefaultLimit = 5
	toolSearchMaxLimit     = 20
	toolSearchMaxQuery     = 256
	toolSearchMaxNames     = 16
	// toolSearchMaxDetails bounds the descriptions or schemas in one reply.
	toolSearchMaxDetails = 4
	// toolSearchLoadScore is the relevance a query result needs before it is
	// loaded. Name and category matches qualify; a single incidental word
	// in a long description does not.
	toolSearchLoadScore = 25
)

//go:embed tool_search.md
var toolSearchDescription string

type ToolSearchParams struct {
	Query  string   `json:"query,omitempty" description:"Short capability words in English or the user's language, e.g. \"browser screenshot\", \"git diff\", \"masaüstü pencere\"."`
	Names  []string `json:"names,omitempty" description:"Exact tool names to load, e.g. [\"browser\"]. Use names listed in the deferred tool index."`
	Limit  int      `json:"limit,omitempty" description:"Maximum query results, 1-20; default 5."`
	Detail string   `json:"detail,omitempty" description:"summary (default), description or schema. Details require exact names (up to 4 per reply; the rest are listed for a follow-up call) and return the live authorized tool definition without running it."`
}

// ToolSearchOptions wires tool_search to the live authorized palette of the
// calling agent.
type ToolSearchOptions struct {
	// Palette returns the agent's current authorized tools. It is called
	// on every search, so a disabled tool or disconnected MCP server is
	// never found.
	Palette func(context.Context) []fantasy.AgentTool
	// Deferred reports whether schemas outside the core palette are loaded
	// on demand. When false every authorized tool is already loaded.
	Deferred func(context.Context) bool
	// Load records tools to expose from the next step on. It must not run
	// them or grant anything.
	Load func(context.Context, []string) error
	// Observe, when set, is told how each capability query ended: how
	// many results it had and how many closest-match suggestions. It is
	// for harness statistics and must not affect the search.
	Observe func(ctx context.Context, query string, results, closest int)
}

type toolSearchResult struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Summary  string `json:"summary"`
	Loaded   bool   `json:"loaded"`
}

// NewToolSearchTool searches the current authorized palette, never all
// tools, and returns short summaries instead of schemas.
func NewToolSearchTool(opts ToolSearchOptions) fantasy.AgentTool {
	return fantasy.NewParallelAgentTool(ToolSearchToolName, toolSearchDescription, func(ctx context.Context, p ToolSearchParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if p.Detail != "" && p.Detail != "summary" && p.Detail != "description" && p.Detail != "schema" {
			return fantasy.NewTextErrorResponse("detail must be summary, description or schema"), nil
		}
		if p.Detail == "description" || p.Detail == "schema" {
			if len(p.Names) < 1 || p.Query != "" {
				return fantasy.NewTextErrorResponse("description/schema detail requires exact names and no query"), nil
			}
		}
		query := slices.DeleteFunc(searchTokens(p.Query), func(word string) bool { return slices.Contains(searchStopwords, word) })
		if len(p.Query) > toolSearchMaxQuery || len(p.Names) > toolSearchMaxNames || p.Limit < 0 || p.Limit > toolSearchMaxLimit {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("query must be at most %d bytes, names at most %d entries and limit 1-%d", toolSearchMaxQuery, toolSearchMaxNames, toolSearchMaxLimit)), nil
		}
		if len(query) == 0 && len(p.Names) == 0 {
			return fantasy.NewTextErrorResponse(`provide a capability query such as {"query":"browser"} or exact names such as {"names":["browser"]}`), nil
		}
		if p.Limit == 0 {
			p.Limit = toolSearchDefaultLimit
		}
		deferred := opts.Deferred != nil && opts.Deferred(ctx)
		palette := ScopeToolsForDesktop(ctx, opts.Palette(ctx))
		// A query made only of exact tool names asks for those tools. Treat it
		// like names, so related tools that merely share a word are listed
		// as suggestions instead of being loaded alongside.
		names := p.Names
		if exact := exactNameQuery(p.Query, palette); len(exact) > 0 {
			names = slices.Concat(names, exact)
		}

		type candidate struct {
			info  fantasy.ToolInfo
			score int
		}
		var named []fantasy.ToolInfo
		var ranked []candidate
		available := map[string]bool{}
		for _, tool := range palette {
			if err := ctx.Err(); err != nil {
				return fantasy.ToolResponse{}, err
			}
			info := tool.Info()
			if info.Name == ToolSearchToolName {
				continue
			}
			available[info.Name] = true
			if slices.Contains(names, info.Name) {
				named = append(named, info)
				continue
			}
			if len(query) > 0 {
				if score := toolSearchScore(info, query); score > 0 {
					ranked = append(ranked, candidate{info, score})
				}
			}
		}
		slices.SortFunc(ranked, func(a, b candidate) int {
			if a.score != b.score {
				return b.score - a.score
			}
			return strings.Compare(a.info.Name, b.info.Name)
		})
		total := len(ranked)
		ranked = ranked[:min(len(ranked), p.Limit)]

		var load []string
		results := make([]toolSearchResult, 0, len(named)+len(ranked))
		for _, info := range named {
			load = append(load, info.Name)
			results = append(results, toolSearchResult{info.Name, ToolCategory(info.Name), toolSummary(info.Description), true})
		}
		exactRequest := len(names) > len(p.Names)
		for _, c := range ranked {
			strong := !exactRequest && c.score >= toolSearchLoadScore
			if strong {
				load = append(load, c.info.Name)
			}
			results = append(results, toolSearchResult{c.info.Name, ToolCategory(c.info.Name), toolSummary(c.info.Description), strong || !deferred})
		}
		if deferred && len(load) > 0 && opts.Load != nil {
			if err := opts.Load(ctx, load); err != nil {
				return fantasy.NewTextErrorResponse("could not load tool schemas: " + err.Error()), nil
			}
		}

		var unknown []string
		for _, name := range p.Names {
			if !available[name] && name != ToolSearchToolName {
				unknown = append(unknown, name)
			}
		}
		next := "All listed tools are already callable."
		if deferred {
			next = "Tools with loaded=true become callable on your next step with their full schemas. To load a listed tool with loaded=false, call tool_search with its exact name in names."
		}
		response := map[string]any{"results": results, "total_matches": total + len(named), "next": next}
		if p.Detail == "description" || p.Detail == "schema" {
			details := map[string]any{}
			var later []string
			for _, info := range named {
				// Details are large; more than a few per call would crowd
				// the context. The rest still load, and their details come
				// with the next call instead of an error that loads nothing.
				if len(details) == toolSearchMaxDetails {
					later = append(later, info.Name)
					continue
				}
				entry := map[string]any{"description": info.Description}
				if p.Detail == "schema" {
					data, err := json.Marshal(map[string]any{"type": "object", "properties": info.Parameters, "required": info.Required})
					if err != nil {
						return fantasy.ToolResponse{}, err
					}
					var definition map[string]any
					if err := json.Unmarshal(data, &definition); err != nil {
						return fantasy.ToolResponse{}, err
					}
					schema.Normalize(definition)
					entry["input_schema"] = definition
					entry["schema_scope"] = "authorized definition; the advertised schema may be narrowed until related tools are loaded"
				}
				details[info.Name] = entry
			}
			response["details"] = details
			if len(later) > 0 {
				response["details_later"] = later
				response["details_note"] = fmt.Sprintf("Details come for %d tools per call; call tool_search again with these names and the same detail for theirs.", toolSearchMaxDetails)
			}
		}
		if len(unknown) > 0 {
			response["unavailable"] = unknown
			response["unavailable_reason"] = "not an enabled tool for this agent; it may be disabled, not allowed or disconnected"
		}
		var closest []toolSearchResult
		if len(results) == 0 {
			response["next"] = "No enabled tool matched. Try a broader capability word (for example file, shell, web, browser, desktop, git, test, memory) or continue with the loaded tools."
			if len(query) > 0 {
				for _, info := range closestTools(palette, query) {
					closest = append(closest, toolSearchResult{info.Name, ToolCategory(info.Name), toolSummary(info.Description), !deferred})
				}
				if len(closest) > 0 {
					response["closest"] = closest
					response["next"] = "No tool matched every word, but these are spelled like your request. If one fits, load it with its exact name in names; otherwise try a broader capability word or continue with the loaded tools."
				}
			}
		}
		if opts.Observe != nil && strings.TrimSpace(p.Query) != "" {
			opts.Observe(ctx, strings.TrimSpace(p.Query), len(results), len(closest))
		}
		data, err := json.Marshal(response)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return fantasy.NewTextResponse(string(data)), nil
	})
}

// exactNameQuery returns the palette tools a query names when every word of
// the query is an exact tool name, ignoring case. Any other word makes it a
// capability search and the result is empty.
func exactNameQuery(query string, palette []fantasy.AgentTool) []string {
	words := strings.FieldsFunc(query, func(r rune) bool { return unicode.IsSpace(r) || r == ',' })
	if len(words) == 0 {
		return nil
	}
	var out []string
	for _, word := range words {
		i := slices.IndexFunc(palette, func(tool fantasy.AgentTool) bool {
			name := tool.Info().Name
			return name != ToolSearchToolName && strings.EqualFold(name, word)
		})
		if i < 0 {
			return nil
		}
		if name := palette[i].Info().Name; !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}
