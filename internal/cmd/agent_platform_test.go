package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func platformCommandTest(t *testing.T, create func() *cobra.Command, root, dir string, args ...string) string {
	t.Helper()
	command := create()
	command.PersistentFlags().String("cwd", root, "fixture")
	command.PersistentFlags().String("data-dir", dir, "fixture")
	command.SetArgs(args)
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	require.NoError(t, command.ExecuteContext(t.Context()))
	return out.String()
}

func TestPlatformCLIStoresJobsWithoutExecutingWorkspaceConfig(t *testing.T) {
	t.Parallel()
	root, dir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "atlasrc"), []byte("exit 99\n"), 0o600))
	platformCommandTest(t, newAgentJobsCommand, root, dir, "add", "ci", "--prompt", "Check CI", "--every", "1m")
	output := platformCommandTest(t, newAgentJobsCommand, root, dir, "list")
	var jobs []agentstate.Job
	require.NoError(t, json.Unmarshal([]byte(output), &jobs))
	require.Len(t, jobs, 1)
	require.True(t, jobs[0].Paused)
	platformCommandTest(t, newAgentJobsCommand, root, dir, "resume", "ci")
	output = platformCommandTest(t, newAgentJobsCommand, root, dir, "list")
	require.NoError(t, json.Unmarshal([]byte(output), &jobs))
	require.False(t, jobs[0].Paused)
	platformCommandTest(t, newTaskBoardCommand, root, dir, "add", "fix", "--title", "Fix", "--prompt", "Fix a bug", "--acceptance", "tests pass")
	output = platformCommandTest(t, newTaskBoardCommand, root, dir, "list")
	var tasks []agentstate.Task
	require.NoError(t, json.Unmarshal([]byte(output), &tasks))
	require.Len(t, tasks, 1)
	require.Equal(t, "ready", tasks[0].Status)
}
