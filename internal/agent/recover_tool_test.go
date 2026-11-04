package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// An ordinary tool error comes back as an error response the model can read;
// a returned error would end the turn.
func TestRecoverableToolReportsAnOrdinaryErrorToTheModel(t *testing.T) {
	t.Parallel()
	wrapped := wrapToolsWithRecovery([]fantasy.AgentTool{&funcTool{
		name: "write",
		run: func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, errors.New("open main.go: access is denied")
		},
	}})

	resp, err := wrapped[0].Run(t.Context(), fantasy.ToolCall{Name: "write"})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.False(t, resp.StopTurn)
	require.Contains(t, resp.Content, `"write"`)
	require.Contains(t, resp.Content, "access is denied")
}

func TestRecoverableToolPassesThroughResponses(t *testing.T) {
	t.Parallel()
	want := fantasy.NewTextErrorResponse("not found")
	want.StopTurn = true
	wrapped := wrapToolsWithRecovery([]fantasy.AgentTool{&funcTool{
		name: "view",
		run: func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return want, nil
		},
	}})

	resp, err := wrapped[0].Run(t.Context(), fantasy.ToolCall{Name: "view"})
	require.NoError(t, err)
	require.Equal(t, want, resp)
}

// The user pressing escape must still end the run.
func TestRecoverableToolPropagatesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	wrapped := wrapToolsWithRecovery([]fantasy.AgentTool{&funcTool{
		name: "bash",
		run: func(ctx context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, fmt.Errorf("run: %w", ctx.Err())
		},
	}})

	_, err := wrapped[0].Run(ctx, fantasy.ToolCall{Name: "bash"})
	require.ErrorIs(t, err, context.Canceled)

	// A wrapped cancellation from a still-live context is also kept.
	wrapped = wrapToolsWithRecovery([]fantasy.AgentTool{&funcTool{
		name: "bash",
		run: func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, fmt.Errorf("run: %w", context.Canceled)
		},
	}})
	_, err = wrapped[0].Run(t.Context(), fantasy.ToolCall{Name: "bash"})
	require.ErrorIs(t, err, context.Canceled)
}
