package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/knowledge"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeToolProposesButCannotApprove(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644))
	store := knowledge.New(filepath.Join(root, ".atlas", "knowledge"), root)
	tool := NewKnowledgeTool(store)
	run := func(p KnowledgeParams) fantasy.ToolResponse {
		in, err := json.Marshal(p)
		require.NoError(t, err)
		res, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "c", Name: KnowledgeToolName, Input: string(in)})
		require.NoError(t, err)
		return res
	}
	res := run(KnowledgeParams{Action: "propose", Title: "Build", Summary: "how to build", Body: "go build .", Sources: []string{"a.go"}})
	require.False(t, res.IsError, res.Content)
	require.Contains(t, res.Content, "until the user approves")
	require.Empty(t, store.Index())
	require.Contains(t, run(KnowledgeParams{Action: "list"}).Content, "build [proposed]")
	require.Contains(t, run(KnowledgeParams{Action: "read", ID: "build"}).Content, "go build .")
	require.True(t, run(KnowledgeParams{Action: "approve", ID: "build"}).IsError)
	require.True(t, run(KnowledgeParams{Action: "read", ID: "nope"}).IsError)
}
