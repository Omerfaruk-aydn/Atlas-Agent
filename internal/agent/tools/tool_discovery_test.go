package tools

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestDeferredPaletteCannotRestoreDisabledTool(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	search := NewToolSearchTool(func(context.Context) []fantasy.AgentTool { return nil })
	optional := fantasy.NewAgentTool("optional", "fixture", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.ToolResponse{}, nil
	})
	palette := []fantasy.AgentTool{search, optional}
	selected, err := DeferredPalette(t.Context(), store, "session", palette)
	require.NoError(t, err)
	require.Len(t, selected, 1)
	require.NoError(t, DiscoverTools(t.Context(), store, "session", []fantasy.ToolInfo{optional.Info()}))
	selected, err = DeferredPalette(t.Context(), store, "session", palette)
	require.NoError(t, err)
	require.Len(t, selected, 2)
	selected, err = DeferredPalette(t.Context(), store, "session", []fantasy.AgentTool{search})
	require.NoError(t, err)
	require.Len(t, selected, 1)
	selected, err = DeferredPalette(t.Context(), store, "other-session", palette)
	require.NoError(t, err)
	require.Len(t, selected, 1)
}
