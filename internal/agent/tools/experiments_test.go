package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func experimentFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	command := exec.CommandContext(t.Context(), "git", "init", root)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return root
}

func TestExperimentRequiresObservedExitAndPreservesDenial(t *testing.T) {
	t.Parallel()
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing observation", true: "denied"}[denied], func(t *testing.T) {
			t.Parallel()
			palette := NewExperimentTools(experimentFixture(t), engineering.NewStore(t.TempDir()), func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				if denied {
					response := fantasy.NewTextErrorResponse("Denied")
					response.StopTurn = true
					return response, nil
				}
				return fantasy.NewTextResponse("failure"), nil
			})
			response, err := palette[0].Run(t.Context(), fantasy.ToolCall{Input: `{"argv":["fixture"],"expected_exit":1,"failure_marker":"failure"}`})
			require.NoError(t, err)
			require.True(t, response.IsError)
			require.Equal(t, denied, response.StopTurn)
		})
	}
}

func TestExperimentMinimizesAndPersistsSourceBoundHistory(t *testing.T) {
	t.Parallel()
	root := experimentFixture(t)
	palette := NewExperimentTools(root, engineering.NewStore(t.TempDir()), func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BashParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		exit := 0
		output := "ok"
		if len(p.Argv) > 1 && p.Argv[1] == "FAULT\n" {
			exit = 3
			output = "signature"
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(output), BashResponseMetadata{ExitCode: &exit, Output: output, StartTime: 1, EndTime: 2}), nil
	})
	response, err := palette[1].Run(t.Context(), fantasy.ToolCall{Input: `{"argv":["fixture","{input}"],"input":"FAULT\n","expected_exit":3,"failure_marker":"signature"}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	response, err = palette[3].Run(t.Context(), fantasy.ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Contains(t, response.Content, `"current_source":true`)
	require.Contains(t, response.Content, "repro_minimize")
}
