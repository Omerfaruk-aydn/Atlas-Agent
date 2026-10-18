package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestToolSearchDetailsNeverExecuteOrExposeDisabledTools(t *testing.T) {
	t.Parallel()
	ran := false
	probe := fantasy.NewAgentTool("probe", "Full procedure: observe, then verify.", func(context.Context, ToolSearchParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		ran = true
		return fantasy.NewTextResponse("unused"), nil
	})
	search := NewToolSearchTool(ToolSearchOptions{Palette: func(context.Context) []fantasy.AgentTool { return []fantasy.AgentTool{probe} }})
	r, err := search.Run(t.Context(), fantasy.ToolCall{Input: `{"names":["probe","disabled"],"detail":"schema"}`})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Contains(t, r.Content, "Full procedure")
	require.Contains(t, r.Content, "input_schema")
	require.Contains(t, r.Content, `"unavailable":["disabled"]`)
	require.False(t, ran)
	r, err = search.Run(t.Context(), fantasy.ToolCall{Input: `{"query":"probe","detail":"schema"}`})
	require.NoError(t, err)
	require.True(t, r.IsError)
}

func TestToolSearchDetailsOverflowIsListedNotRefused(t *testing.T) {
	t.Parallel()
	var palette []fantasy.AgentTool
	var names []string
	for _, name := range []string{"t1", "t2", "t3", "t4", "t5", "t6"} {
		palette = append(palette, fantasy.NewAgentTool(name, "Tool "+name+".", func(context.Context, ToolSearchParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("unused"), nil
		}))
		names = append(names, `"`+name+`"`)
	}
	search := NewToolSearchTool(ToolSearchOptions{Palette: func(context.Context) []fantasy.AgentTool { return palette }})
	r, err := search.Run(t.Context(), fantasy.ToolCall{Input: `{"names":[` + strings.Join(names, ",") + `],"detail":"description"}`})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Contains(t, r.Content, `"details_later":["t5","t6"]`)
}
