package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentCLIInspectionDoesNotRunProjectConfiguration(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\ngo 1.26.6\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "atlasrc"), []byte("printf unexpected > marker.txt\n"), 0o644))
	var output bytes.Buffer
	command := newWorkflowCommand()
	command.PersistentFlags().String("cwd", "", "")
	command.PersistentFlags().String("data-dir", "", "")
	command.SetOut(&output)
	command.SetArgs([]string{"environment", "inspect", "--cwd", root, "--data-dir", t.TempDir()})
	require.NoError(t, command.ExecuteContext(t.Context()))
	require.Contains(t, output.String(), "source_fingerprint")
	_, err := os.Stat(filepath.Join(root, "marker.txt"))
	require.True(t, os.IsNotExist(err), "Read-only inspection must not execute atlasrc")
}
