package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSecretFilterPreservesStructuredMetadata(t *testing.T) {
	t.Parallel()
	session := uuid.NewString()
	require.NoError(t, vault.Register(session, "true"))
	ctx := context.WithValue(t.Context(), SessionIDContextKey, session)
	tool := fantasy.NewAgentTool("fixture", "Synthetic fixture.", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.ToolResponse{
			Content:  "true",
			Metadata: `{"background":true,"count":9007199254740993,"output":"true","nested":["true",false]}`,
		}, nil
	})
	resp, err := WithSecretFilter(tool).Run(ctx, fantasy.ToolCall{Input: "{}"})
	require.NoError(t, err)
	require.Equal(t, "[REDACTED]", resp.Content)
	var metadata struct {
		Background bool   `json:"background"`
		Count      int64  `json:"count"`
		Output     string `json:"output"`
		Nested     []any  `json:"nested"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &metadata))
	require.True(t, metadata.Background)
	require.EqualValues(t, 9007199254740993, metadata.Count)
	require.Equal(t, "[REDACTED]", metadata.Output)
	require.Equal(t, []any{"[REDACTED]", false}, metadata.Nested)
	// Only nested bash dispatch is eligible for internal raw evidence.
	ctx = context.WithValue(ctx, cliBridgeOutputContextKey{}, true)
	resp, err = WithSecretFilter(tool).Run(ctx, fantasy.ToolCall{Input: "{}"})
	require.NoError(t, err)
	require.Equal(t, "[REDACTED]", resp.Content)
}
