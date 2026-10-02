package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScenarioCLIValidationDoesNotExecuteConfigOrCreateDatabase(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "atlasrc"), []byte("touch should-not-exist\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "scenario.json"), []byte(`{"id":"fixture","version":1,"target":"web","url":"http://localhost:3000","width":800,"height":600,"steps":[],"assertions":[{"kind":"visible","selector":"body"}]}`), 0o644))
	command := newWorkflowCommand()
	command.PersistentFlags().String("cwd", root, "")
	command.PersistentFlags().String("data-dir", "", "")
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs([]string{"scenario", "validate", "scenario.json"})
	require.NoError(t, command.ExecuteContext(t.Context()))
	require.Contains(t, out.String(), `"executed":false`)
	_, err := os.Stat(filepath.Join(root, "should-not-exist"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(root, ".atlas"))
	require.True(t, os.IsNotExist(err))
	command = newWorkflowCommand()
	command.PersistentFlags().String("cwd", root, "")
	command.SetArgs([]string{"scenario", "run", "scenario.json", "--session-id", "fixture"})
	require.ErrorContains(t, command.ExecuteContext(t.Context()), "--allow-execution")
	_, err = os.Stat(filepath.Join(root, ".atlas"))
	require.True(t, os.IsNotExist(err))
}
