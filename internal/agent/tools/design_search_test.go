package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestDesignSearchTool(t *testing.T) {
	t.Parallel()
	tool := NewDesignSearchTool()
	input, err := json.Marshal(DesignSearchParams{Query: "healthcare accessible clean", DesignSystem: true, Limit: 2})
	require.NoError(t, err)
	response, err := tool.Run(context.Background(), fantasy.ToolCall{ID: "design-1", Name: DesignSearchToolName, Input: string(input)})
	require.NoError(t, err)
	require.False(t, response.IsError)
	var result designSearchOutput
	require.NoError(t, json.Unmarshal([]byte(response.Content), &result))
	require.Len(t, result.Results, 7)
	for _, rows := range result.Results {
		require.LessOrEqual(t, len(rows), 2)
	}
	for _, params := range []DesignSearchParams{
		{Query: "clean", Domain: "../../secret"},
		{Query: "clean", Domain: "stack", Stack: "../../secret"},
		{Query: "clean", Domain: "ux", Limit: 100},
	} {
		input, err := json.Marshal(params)
		require.NoError(t, err)
		response, err := tool.Run(context.Background(), fantasy.ToolCall{ID: "design-2", Name: DesignSearchToolName, Input: string(input)})
		require.NoError(t, err)
		require.True(t, response.IsError)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = tool.Run(ctx, fantasy.ToolCall{ID: "design-3", Name: DesignSearchToolName, Input: `{"query":"clean","domain":"style"}`})
	require.ErrorIs(t, err, context.Canceled)
}

func TestDesignSearchOutputBound(t *testing.T) {
	t.Parallel()
	rows := []skills.DesignMatch{{Fields: map[string]string{"example": strings.Repeat("界", 20000)}}}
	data, err := boundedDesignOutput(map[string][]skills.DesignMatch{"style": rows}, []string{"style"})
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), 32*1024)
	var result designSearchOutput
	require.NoError(t, json.Unmarshal(data, &result))
	require.True(t, result.Truncated)
	require.Equal(t, 1, result.Omitted)
	require.Empty(t, result.Results["style"])
}
