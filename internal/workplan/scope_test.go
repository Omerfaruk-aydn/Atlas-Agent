package workplan

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const twoFeaturePlan = `{"features":[{"id":"A","title":"a"},{"id":"B","title":"b"}]}`

func TestSessionsKeepTheirOwnProgress(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePlan(t, root, twoFeaturePlan, "")

	require.NoError(t, UpdateState(root, "s1", func(s *State) error {
		s.ReviewedPlan, s.Settled = "plan-v1", []string{"A"}
		return nil
	}))
	require.NoError(t, UpdateState(root, "s2", func(s *State) error {
		s.ReviewedPlan, s.Settled = "plan-v2", []string{"B"}
		return nil
	}))

	first, second := LoadSessionState(root, "s1"), LoadSessionState(root, "s2")
	require.Equal(t, "plan-v1", first.ReviewedPlan, "the second session must not overwrite the first's progress")
	require.Equal(t, []string{"A"}, first.Settled)
	require.Equal(t, "plan-v2", second.ReviewedPlan)
	require.Equal(t, []string{"B"}, second.Settled)
	require.Empty(t, LoadState(root).ReviewedPlan, "session progress never leaks into the shared file")
}

func TestFlakyKnowledgeIsSharedAcrossSessions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session := fmt.Sprintf("s%d", i)
			require.NoError(t, UpdateState(root, session, func(s *State) error {
				s.AddFlaky(fmt.Sprintf("go test ./pkg%d", i))
				return nil
			}))
		}()
	}
	wg.Wait()

	// Any session sees what all of them learned; none was lost to a race.
	state := LoadSessionState(root, "fresh")
	require.Len(t, state.Flaky, 6)
}

func TestConcurrentUpdatesNeverLoseAWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	var wg sync.WaitGroup
	for i := range 25 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, UpdateState(root, "same", func(s *State) error {
				s.Settled = append(s.Settled, fmt.Sprintf("F%d", i))
				return nil
			}))
		}()
	}
	wg.Wait()
	require.Len(t, LoadSessionState(root, "same").Settled, 25, "read-modify-write must be serialized")
}

func TestTurnCostsAreRecordedPerSessionAndSummed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePlan(t, root, twoFeaturePlan, "")
	var wg sync.WaitGroup
	for _, session := range []string{"s1", "s2"} {
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				require.NoError(t, AddTurnFor(root, session, time.Minute, 100))
			}()
		}
	}
	wg.Wait()

	total, err := LoadCosts(root)
	require.NoError(t, err)
	require.Equal(t, 10, total["A"].Turns, "no session's turns are lost, and the project sees the sum")
	require.EqualValues(t, 1000, total["A"].Tokens)
	own, err := loadCostsAt(filepath.Join(sessionDir(root, "s1"), MetricsFile))
	require.NoError(t, err)
	require.Equal(t, 5, own["A"].Turns)
}

func TestAtomicWriteLeavesNoPartialFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "x", "state.json")
	require.NoError(t, writeFileAtomic(path, []byte(`{"a":1}`), 0o644))
	require.NoError(t, writeFileAtomic(path, []byte(`{"a":2}`), 0o644))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.JSONEq(t, `{"a":2}`, string(data))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "temporary files are cleaned up: %v", entries)
}

func TestACorruptSessionFileIsAnEmptyStateNotAFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := sessionDir(root, "s1")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, StateFile), []byte(`{"reviewed_plan": "tru`), 0o644))
	require.Empty(t, LoadSessionState(root, "s1").ReviewedPlan)
	require.NoError(t, UpdateState(root, "s1", func(s *State) error { s.ReviewedPlan = "ok"; return nil }))
	require.Equal(t, "ok", LoadSessionState(root, "s1").ReviewedPlan)
}

// A lock file left on disk by an earlier run holds nothing: the lock is the
// kernel's, so only a live holder excludes anyone.
func TestALeftoverLockFileDoesNotBlock(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path+".lock", []byte("999999\n"), 0o600))

	ran := false
	require.NoError(t, withFileLock(path, func() error { ran = true; return nil }))
	require.True(t, ran)
}

func TestSessionIDsCannotEscapeTheirDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := sessionDir(root, "../../etc/passwd")
	rel, err := filepath.Rel(filepath.Join(root, Dir, SessionsDir), dir)
	require.NoError(t, err)
	require.NotContains(t, rel, "..")
	require.NotEqual(t, sessionDir(root, "a/b"), sessionDir(root, "a_b"), "distinct ids stay distinct after sanitizing")
}

func TestASessionStartsFromTheProjectWideState(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, SaveState(root, State{ReviewedPlan: "legacy", Settled: []string{"A"}}))
	require.Equal(t, []string{"A"}, LoadSessionState(root, "new").Settled, "work recorded before sessions existed is not forgotten")
}
