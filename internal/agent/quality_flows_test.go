package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestQualityRollbackInvalidatesGenerationOnlyWhenApplied(t *testing.T) {
	t.Parallel()
	for _, applied := range []bool{false, true} {
		store := engineering.NewStore(t.TempDir())
		ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "session")
		inner := fantasy.NewAgentTool("undo_feature", "fixture", func(context.Context, tools.UndoFeatureParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse("result"), tools.UndoFeatureMetadata{Applied: applied}), nil
		})
		response, err := (&guardedTool{AgentTool: inner, store: store}).Run(ctx, fantasy.ToolCall{ID: "rollback", Name: "undo_feature", Input: `{"action":"prepare"}`})
		require.NoError(t, err)
		require.False(t, response.IsError)
		state, err := store.Read(ctx, "session")
		require.NoError(t, err)
		if applied {
			require.EqualValues(t, 1, state.Generation)
		} else {
			require.Zero(t, state.Generation)
		}
	}
}

func TestQualityBoundaryPreparationCompletesWithoutVerificationEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(root, "prepare.json"), []byte(`{"version":1,"action":"prepare","labels":["customer"]}`), 0o600))
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "session")
	inner := tools.NewDataBoundaryTool(root, store)
	call := fantasy.ToolCall{ID: "prepare", Name: "data_boundary", Input: `{"manifest":"prepare.json"}`}
	response, err := (&guardedTool{AgentTool: inner, store: store}).Run(ctx, call)
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	state, err := store.Read(ctx, "session")
	require.NoError(t, err)
	require.Empty(t, state.Failures)
	require.Len(t, state.Operations, 1)
	require.Equal(t, "completed", state.Operations[0].Status)
	require.False(t, state.Operations[0].OutcomeObserved)
	require.False(t, tools.ToolSucceeded("data_boundary", response, nil))
	require.False(t, tools.ToolOutcomeObserved("data_boundary", response, nil))
	verify := tools.NewVerifyTool(root, store, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) { return response, nil })
	result, err := verify.Run(ctx, fantasy.ToolCall{ID: "verify", Name: "verify", Input: `{"action":"run","checks":[{"name":"not a real scan","tool":"data_boundary","input":{"manifest":"prepare.json"}}]}`})
	require.NoError(t, err)
	require.True(t, result.IsError, result.Content)
}
