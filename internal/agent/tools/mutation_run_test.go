package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestMutationChecksSourceEvenWhenEngineFails(t *testing.T) {
	t.Parallel()
	root := experimentFixture(t)
	source := filepath.Join(root, "source.go")
	require.NoError(t, os.WriteFile(source, []byte("package fixture\n"), 0o644))
	store := engineering.NewStore(filepath.Join(root, ".atlas"))
	runs := 0
	tool := NewMutationTestTool(root, store, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		runs++
		exit := 0
		if runs == 2 {
			require.NoError(t, os.WriteFile(source, []byte("package broken\n"), 0o644))
			exit = 1
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("observed"), BashResponseMetadata{ExitCode: &exit, StartTime: 1, EndTime: 2}), nil
	})
	response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: `{"baseline_argv":["baseline"],"mutation_argv":["engine"],"report_path":".atlas/engineering/fresh.json"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "source integrity")
	require.Equal(t, 2, runs)
}
