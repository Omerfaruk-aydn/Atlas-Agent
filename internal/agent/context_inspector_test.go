package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestContextInspectorExcludesResultsWithoutMutatingHistory(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	a := &sessionAgent{engineering: store}
	message := fantasy.Message{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: "result", Output: fantasy.ToolResultOutputContentText{Text: "original output"}}}}
	var pins []string
	_, err := a.inspectContext(t.Context(), "session", "model", 0, []fantasy.Message{message}, nil, &pins)
	require.NoError(t, err)
	require.NoError(t, store.ContextControl(t.Context(), t.TempDir(), "session", "context_exclude", "result"))
	out, err := a.inspectContext(t.Context(), "session", "model", 1, []fantasy.Message{message}, nil, &pins)
	require.NoError(t, err)
	result, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](out[0].Content[0])
	require.True(t, ok)
	require.Equal(t, "result", result.ToolCallID)
	text, _ := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result.Output)
	require.Contains(t, text.Text, "excluded by user")
	original, _ := fantasy.AsMessagePart[fantasy.ToolResultPart](message.Content[0])
	originalText, _ := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](original.Output)
	require.Equal(t, "original output", originalText.Text)
	require.Error(t, store.ContextControl(t.Context(), t.TempDir(), "session", "context_exclude", "mandatory-system"))
}

func TestContextPinsAreBoundedAndContained(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.txt"), []byte("source"), 0o600))
	store := engineering.NewStore(t.TempDir())
	require.NoError(t, store.ContextControl(t.Context(), root, "session", "context_pin", "source.txt"))
	require.Error(t, store.ContextControl(t.Context(), root, "session", "context_pin", "../outside.txt"))
	_, prefs, err := store.ReadContext(t.Context(), "session")
	require.NoError(t, err)
	require.Equal(t, []string{"source.txt"}, prefs.PinnedPaths)
}

func TestContextInspectorListsInstructionSourcesWithoutDoubleCounting(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	a := &sessionAgent{engineering: store}
	var pins []string
	input := fantasy.NewSystemMessage(`<project_context><file path="AGENTS.md" origin="project" scope=".">Follow project rules.</file></project_context>`)
	_, err := a.inspectContext(t.Context(), "session", "model", 0, []fantasy.Message{input}, nil, &pins)
	require.NoError(t, err)
	manifest, _, err := store.ReadContext(t.Context(), "session")
	require.NoError(t, err)
	require.Len(t, manifest.Entries, 2)
	require.Equal(t, "AGENTS.md", manifest.Entries[1].Name)
	require.Equal(t, "instruction-source", manifest.Entries[1].Kind)
	require.NotEmpty(t, manifest.Entries[1].ParentID)
	require.Equal(t, manifest.Entries[0].EstimatedTokens, manifest.EstimatedTokens)
}
