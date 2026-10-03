package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestSelectionBoundsContextAndDisclosesTestGaps(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "app.ts"), []byte(strings.Repeat("export const value = 'ç';\n", 200)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app.test.ts"), []byte("test('fixture',()=>{});"), 0o644))
	palette := NewSelectionTools(root, engineering.NewStore(t.TempDir()))
	response, err := palette[0].Run(t.Context(), fantasy.ToolCall{Input: `{"changed_paths":["app.ts"],"token_budget":128}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	var selected struct {
		Files           []selectedFile
		EstimatedTokens int `json:"estimated_tokens"`
	}
	require.NoError(t, json.Unmarshal([]byte(response.Content), &selected))
	require.LessOrEqual(t, selected.EstimatedTokens, 128)
	require.NotEmpty(t, selected.Files)
	require.NotEmpty(t, selected.Files[0].Content)
	response, err = palette[1].Run(t.Context(), fantasy.ToolCall{Input: `{"changed_paths":["app.ts"]}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Contains(t, response.Content, "app.test.ts")
	require.Contains(t, response.Content, `"full_suite_required":true`)
}
