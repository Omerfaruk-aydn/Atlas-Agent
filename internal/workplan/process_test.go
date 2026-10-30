package workplan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
	"github.com/stretchr/testify/require"
)

// These tests run the writers as separate operating-system processes: the
// test binary starts itself with a role in the environment (see TestMain). A
// goroutine test cannot show what two Atlas processes on one project do.

const childEnv = "ATLAS_WORKPLAN_CHILD"

func TestMain(m *testing.M) {
	if role := os.Getenv(childEnv); role != "" {
		os.Exit(runChild(role))
	}
	os.Exit(m.Run())
}

func runChild(role string) int {
	root, session := os.Getenv("CHILD_ROOT"), os.Getenv("CHILD_SESSION")
	n, _ := strconv.Atoi(os.Getenv("CHILD_N"))
	tag := fmt.Sprintf("%s-%d", session, os.Getpid())
	switch role {
	case "write":
		for i := range n {
			if err := UpdateState(root, session, func(s *State) error {
				s.Settled = append(s.Settled, fmt.Sprintf("%s-%d", tag, i))
				if i < 4 {
					s.AddFlaky(fmt.Sprintf("go test ./%s-%d", tag, i))
				}
				return nil
			}); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if err := AddTurnFor(root, session, time.Second, 10); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		return 0
	case "hold":
		// Take the lock, announce it, then wait to be killed.
		path := filepath.Join(sessionDir(root, session), StateFile)
		err := withFileLock(path, func() error {
			if err := os.WriteFile(os.Getenv("CHILD_READY"), nil, 0o644); err != nil {
				return err
			}
			time.Sleep(time.Minute)
			return nil
		})
		fmt.Fprintln(os.Stderr, err)
		return 1
	case "tear":
		// Die between creating the temporary file and renaming it.
		path := filepath.Join(sessionDir(root, session), StateFile)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return 1
		}
		_ = os.WriteFile(path+".tmp-torn", []byte(`{"settled":["half`), 0o644)
		return 3
	}
	return 2
}

func child(t *testing.T, role, root, session string, n int, extra ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0])
	cmd.Env = append(os.Environ(), childEnv+"="+role, "CHILD_ROOT="+root, "CHILD_SESSION="+session, "CHILD_N="+strconv.Itoa(n))
	cmd.Env = append(cmd.Env, extra...)
	return cmd
}

func TestSeparateProcessesLoseNoUpdates(t *testing.T) {
	root := t.TempDir()
	writePlan(t, root, twoFeaturePlan, "")
	const perChild = 25

	// Two processes write the same session while two others write their own.
	runs := []struct {
		session string
		cmd     *exec.Cmd
	}{
		{"shared-session", child(t, "write", root, "shared-session", perChild)},
		{"shared-session", child(t, "write", root, "shared-session", perChild)},
		{"other-a", child(t, "write", root, "other-a", perChild)},
		{"other-b", child(t, "write", root, "other-b", perChild)},
	}
	for _, run := range runs {
		run.cmd.Stderr = os.Stderr
		require.NoError(t, run.cmd.Start())
	}
	for _, run := range runs {
		require.NoError(t, run.cmd.Wait(), "a writer failed or timed out waiting for the lock")
	}

	// No lost update within a session, and sessions stay apart.
	same := LoadSessionState(root, "shared-session")
	require.Len(t, same.Settled, 2*perChild, "two processes, one session: every read-modify-write survived")
	require.Len(t, LoadSessionState(root, "other-a").Settled, perChild)
	require.Len(t, LoadSessionState(root, "other-b").Settled, perChild)
	for _, id := range LoadSessionState(root, "other-a").Settled {
		require.Contains(t, id, "other-a-", "another session's progress leaked in")
	}

	// Project-wide knowledge is the union of what every process learned.
	require.Len(t, LoadState(root).Flaky, 16, "4 processes x 4 flaky commands, none overwritten")
	require.Empty(t, LoadState(root).Settled, "session progress never reaches the shared file")

	// Costs add up across processes and sessions.
	total, err := LoadCosts(root)
	require.NoError(t, err)
	require.Equal(t, 4*perChild, total["A"].Turns)
}

// The kernel releases a lock when its holder dies. A killed process must not
// leave the project unwritable.
func TestAKilledHolderReleasesItsLock(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
	holder := child(t, "hold", root, "s1", 0, "CHILD_READY="+ready)
	require.NoError(t, holder.Start())
	t.Cleanup(func() { _ = holder.Process.Kill(); _ = holder.Wait() })
	require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 10*time.Second, 20*time.Millisecond)

	path := filepath.Join(sessionDir(root, "s1"), StateFile)
	_, err := lock.TryFile(path + ".lock")
	require.ErrorIs(t, err, lock.ErrContended, "while the holder lives, nobody else gets in")

	require.NoError(t, holder.Process.Kill())
	_ = holder.Wait()

	started := time.Now()
	require.NoError(t, UpdateState(root, "s1", func(s *State) error { s.ReviewedPlan = "after-crash"; return nil }))
	require.Less(t, time.Since(started), 3*time.Second, "no waiting for a stale lock to age out")
	require.Equal(t, "after-crash", LoadSessionState(root, "s1").ReviewedPlan)
}

// A process that dies while writing leaves the previous complete state, not a
// half-written one, and the next writer is unaffected.
func TestAProcessKilledMidWriteLeavesTheLastCompleteState(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, UpdateState(root, "s1", func(s *State) error { s.Settled = []string{"A"}; return nil }))

	cmd := child(t, "tear", root, "s1", 0)
	err := cmd.Run()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 3, exit.ExitCode())

	require.Equal(t, []string{"A"}, LoadSessionState(root, "s1").Settled, "the torn temporary file is never read as state")
	require.NoError(t, UpdateState(root, "s1", func(s *State) error { s.Settled = append(s.Settled, "B"); return nil }))
	require.Equal(t, []string{"A", "B"}, LoadSessionState(root, "s1").Settled)
}
