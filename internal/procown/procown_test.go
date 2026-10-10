package procown

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if os.Getenv("PROCOWN_HELPER") == "sleep" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(t *testing.T) Spec {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return Spec{Argv: []string{exe, "-test.run=^$"}, Env: append(os.Environ(), "PROCOWN_HELPER=sleep")}
}

func TestOwnedProcessIdentityAndStop(t *testing.T) {
	p, err := Start(helper(t))
	require.NoError(t, err)
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		require.NotZero(t, p.Identity.CreatedAt)
		require.True(t, Matches(p.Identity))
	}
	p.Stop(2 * time.Second)
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not stop")
	}
	require.False(t, Matches(p.Identity))
	require.ErrorIs(t, KillOwned(p.Identity), ErrNotOwned)
}

func TestForeignProcessWithReusedPIDIsNotOwned(t *testing.T) {
	// A process the user started is never matched by a stale identity.
	cmd := exec.CommandContext(t.Context(), helper(t).Argv[0], "-test.run=^$")
	cmd.Env = helper(t).Env
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	fake := Identity{PID: cmd.Process.Pid, CreatedAt: 12345}
	require.False(t, Matches(fake))
	require.ErrorIs(t, KillOwned(fake), ErrNotOwned)
	// PID alone (no creation time) is not ownership either.
	require.False(t, Matches(Identity{PID: cmd.Process.Pid}))
	_, err := os.FindProcess(cmd.Process.Pid)
	require.NoError(t, err)
}

func TestWaitStopsOnCancel(t *testing.T) {
	p, err := Start(helper(t))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = p.Wait(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
