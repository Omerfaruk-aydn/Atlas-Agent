package tools

import (
	"context"
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func hintFixtureTool(name, description string) fantasy.AgentTool {
	return fantasy.NewAgentTool(name, description, func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.ToolResponse{}, nil
	})
}

type hintSearchResponse struct {
	Results []toolSearchResult `json:"results"`
	Closest []toolSearchResult `json:"closest"`
	Next    string             `json:"next"`
}

func runHintSearch(t *testing.T, palette []fantasy.AgentTool, deferred bool, input string) hintSearchResponse {
	t.Helper()
	var loaded []string
	tool := NewToolSearchTool(ToolSearchOptions{
		Palette:  func(context.Context) []fantasy.AgentTool { return palette },
		Deferred: func(context.Context) bool { return deferred },
		Load: func(_ context.Context, names []string) error {
			loaded = append(loaded, names...)
			return nil
		},
	})
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{Input: input})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	var out hintSearchResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &out))
	for _, c := range out.Closest {
		require.NotContains(t, loaded, c.Name, "a suggestion is never loaded")
	}
	return out
}

func TestToolHintsAreShortAndSayWhen(t *testing.T) {
	t.Parallel()

	for name, hint := range toolHints {
		require.NotEmpty(t, hint, name)
		require.LessOrEqual(t, utf8.RuneCountInString(hint), maxHintRunes, name)
		require.NotEqual(t, name, hint, "a hint must add something to the name")
	}
	require.Equal(t, toolHints["browser"], ToolHint("browser", "ignored"))
}

func TestToolHintFallsBackToTheDescription(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Create a GitHub issue", ToolHint("mcp_github_create_issue", "Create a GitHub issue. Requires a repository."))
	require.Equal(t, "List rows", ToolHint("mcp_db_rows", "List rows (paged): returns at most 100."))
	long := ToolHint("mcp_x", "Synchronize every calendar event between the primary account and all secondary shared calendars nightly")
	require.LessOrEqual(t, utf8.RuneCountInString(long), maxHintRunes+1)
	require.Contains(t, long, "…")
	require.Empty(t, ToolHint("mcp_y", ""))
}

func TestToolSearchFindsToolsByAliasInEitherLanguage(t *testing.T) {
	t.Parallel()

	palette := []fantasy.AgentTool{
		hintFixtureTool("browser", "Built-in Atlas tool: control a browser."),
		hintFixtureTool("computer", "Built-in Atlas tool: control the desktop."),
		hintFixtureTool("fetch", "Fetch a URL."),
	}
	for query, want := range map[string]string{
		"playwright screenshot":       "browser",
		"sayfanın görüntüsüne bak":    "browser",
		"curl html":                   "fetch",
		"pencereye tıkla":             "computer",
		"masaüstündeki uygulamayı aç": "computer",
	} {
		got := runHintSearch(t, palette, true, `{"query":"`+query+`"}`)
		require.NotEmpty(t, got.Results, query)
		require.Equal(t, want, got.Results[0].Name, query)
	}
}

func TestToolSearchSuggestsClosestToolsInsteadOfNothing(t *testing.T) {
	t.Parallel()

	palette := []fantasy.AgentTool{
		hintFixtureTool("browser", "Built-in Atlas tool: control a browser."),
		hintFixtureTool("debugger", "Runs a Go program under Delve."),
		hintFixtureTool("memory", "Remember facts."),
	}
	got := runHintSearch(t, palette, true, `{"query":"breakpoit"}`)
	require.Empty(t, got.Results)
	require.NotEmpty(t, got.Closest, "a typo still finds the tool")
	require.Equal(t, "debugger", got.Closest[0].Name)
	require.False(t, got.Closest[0].Loaded)
	require.Contains(t, got.Next, "exact name in names")

	nothing := runHintSearch(t, palette, true, `{"query":"zzzz qqqq"}`)
	require.Empty(t, nothing.Results)
	require.Empty(t, nothing.Closest, "unrelated words suggest nothing")
	require.Contains(t, nothing.Next, "No enabled tool matched")
}

func TestWordSimilarity(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 1.0, wordSimilarity("browser", "browser"), 0.001)
	require.GreaterOrEqual(t, wordSimilarity("screnshot", "screenshot"), 0.7)
	require.GreaterOrEqual(t, wordSimilarity("goruntusune", "goruntu"), 0.7)
	require.Less(t, wordSimilarity("memory", "browser"), 0.7)
	require.Zero(t, wordSimilarity("ab", "abc"), "short words are ambiguous")
}
