package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/maintenance"
	"github.com/stretchr/testify/require"
)

func TestEditorQueueDeduplicatesCancelsAndRetriesWithoutDispatch(t *testing.T) {
	t.Parallel()
	root, dataDir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "fixture.go"), []byte("package fixture\nconst text = \"// ATLAS: ignore literal\"\n// ATLAS: implement search\n"), 0o644))
	var output bytes.Buffer
	run := func(args ...string) error {
		output.Reset()
		command := newWatchCommentsCommand()
		command.PersistentFlags().String("cwd", root, "test")
		command.PersistentFlags().String("data-dir", dataDir, "test")
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(args)
		return command.ExecuteContext(t.Context())
	}
	require.NoError(t, run("--file", "fixture.go", "--once"))
	var task maintenance.CommentTask
	require.NoError(t, json.Unmarshal(output.Bytes(), &task))
	require.Equal(t, "implement search", task.Prompt)
	require.NoError(t, run("--file", "fixture.go", "--once"))
	require.Empty(t, output.String())
	require.NoError(t, run("cancel", task.ID))
	require.NoError(t, run("list"))
	require.Contains(t, output.String(), `"status":"cancelled"`)
	require.NoError(t, run("retry", task.ID))
	require.NoError(t, run("list"))
	require.Contains(t, output.String(), `"status":"queued"`)
}
