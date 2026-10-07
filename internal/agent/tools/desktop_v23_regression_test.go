package tools

import (
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopRenameAcceptsSemanticHint(t *testing.T) {
	t.Parallel()
	p := renameParams()
	p.Observation = "semantic"
	invoke, _ := renameFixture(t, nil)
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
}
