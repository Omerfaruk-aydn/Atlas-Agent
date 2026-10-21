//go:build windows

package shell

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A job whose command starts a child that inherits its output used to hang
// Kill forever on Windows: only the direct child was ended, and the
// grandchild kept the output pipe open, so the job never finished. job_kill
// and with it the turn (and the user's Stop) waited on it indefinitely.
func TestKillEndsAJobWhoseChildHoldsTheOutput(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	job, err := manager.Start(t.Context(), t.TempDir(), nil, "cmd /c ping -n 120 127.0.0.1", "")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		stdout, _, _, _ := job.GetOutput()
		return len(stdout) > 0
	}, 20*time.Second, 100*time.Millisecond, "ping should start writing")

	started := time.Now()
	require.NoError(t, manager.Kill(job.ID))
	require.Less(t, time.Since(started), killWait, "Kill must not wait out its bound")
	require.True(t, job.IsDone())
}
