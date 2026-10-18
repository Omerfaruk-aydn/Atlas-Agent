package tools

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/stretchr/testify/require"
)

func TestRunningDuplicateIsNotStartedAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	command := "sleep 30 # atlas duplicate test"
	require.Empty(t, runningDuplicate(command, dir))

	manager := shell.GetBackgroundShellManager()
	job, err := manager.Start(context.WithoutCancel(t.Context()), dir, nil, command, "duplicate test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(job.ID) })

	note := runningDuplicate("sleep  30   # atlas duplicate test", dir)
	require.Contains(t, note, "already running as background job "+job.ID)
	require.Empty(t, runningDuplicate(command, t.TempDir()), "another folder is another job")
	require.Empty(t, runningDuplicate("sleep 31", dir))
	require.Contains(t, runningDuplicate(command+" --port 5174", dir), "a different port does not make it a different server")
	require.Equal(t, "npm run dev -- --host 0.0.0.0", normalizeJobCommand("npm run dev -- --host 0.0.0.0 --port 5183 --strictPort"))
	require.Equal(t, "npm run dev", normalizeJobCommand("PORT=3001 npm run dev"))

	require.NoError(t, manager.Kill(job.ID))
	require.Empty(t, runningDuplicate(command, dir), "a stopped job does not block a new run")
}
