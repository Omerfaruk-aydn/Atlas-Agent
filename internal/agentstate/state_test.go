package agentstate

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Release(dir)) })
	return &Store{DB: conn}, dir
}

func TestStateCASAndReopen(t *testing.T) {
	t.Parallel()
	s, dir := testStore(t)
	require.NoError(t, s.Put(t.Context(), "goals", "one", 0, Goal{Text: "finish", Budget: 30, Used: 7}))
	require.ErrorIs(t, s.Put(t.Context(), "goals", "one", 0, Goal{}), ErrConflict)
	var g Goal
	rev, err := s.Get(t.Context(), "goals", "one", &g)
	require.NoError(t, err)
	require.EqualValues(t, 1, rev)
	require.NoError(t, s.Put(t.Context(), "goals", "one", rev, Goal{Text: "finish", Budget: 30, Used: 8}))
	require.ErrorIs(t, s.Put(t.Context(), "goals", "one", rev, Goal{}), ErrConflict)
	require.NoError(t, db.Release(dir))
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	s.DB = conn
	_, err = s.Get(t.Context(), "goals", "one", &g)
	require.NoError(t, err)
	require.Equal(t, 8, g.Used)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, s.Put(ctx, "goals", "one", 2, g))
}

func TestTaskExclusiveClaimAndEvidenceReview(t *testing.T) {
	t.Parallel()
	s, root := testStore(t)
	path := filepath.Join(root, "proof.txt")
	require.NoError(t, os.WriteFile(path, []byte("test passed"), 0o600))
	task := Task{ID: "task", Root: root, Title: "Fix", Prompt: "Fix a bug", Acceptance: []string{"test passes"}}
	require.NoError(t, AddTask(t.Context(), s, "board", task))
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := []Task{}
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, err := ClaimTask(t.Context(), s, "board", "task", string(rune('a'+i)), 300)
			if err == nil {
				mu.Lock()
				claims = append(claims, claim)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Len(t, claims, 1)
	claim := claims[0]
	require.Error(t, TransitionTask(t.Context(), s, "board", "task", "submit", "wrong", []string{"proof.txt"}, "done"))
	require.NoError(t, TransitionTask(t.Context(), s, "board", "task", "submit", claim.Attempt, []string{"proof.txt"}, "done"))
	require.NoError(t, os.WriteFile(path, []byte("different"), 0o600))
	require.ErrorContains(t, TransitionTask(t.Context(), s, "board", "task", "accept", "", nil, ""), "changed")
	require.NoError(t, TransitionTask(t.Context(), s, "board", "task", "retry", "", nil, ""))
	newClaim, err := ClaimTask(t.Context(), s, "board", "task", "new", 300)
	require.NoError(t, err)
	require.NotEqual(t, claim.Attempt, newClaim.Attempt)
	require.NoError(t, TransitionTask(t.Context(), s, "board", "task", "submit", newClaim.Attempt, []string{"proof.txt"}, "retested"))
	require.NoError(t, TransitionTask(t.Context(), s, "board", "task", "accept", "", nil, ""))
}

func TestTaskRecoveryAndDependencies(t *testing.T) {
	t.Parallel()
	s, root := testStore(t)
	require.NoError(t, AddTask(t.Context(), s, "board", Task{ID: "a", Root: root, Title: "A", Prompt: "A", Acceptance: []string{"A"}}))
	require.NoError(t, AddTask(t.Context(), s, "board", Task{ID: "b", Root: root, Title: "B", Prompt: "B", Acceptance: []string{"B"}, Dependencies: []string{"a"}}))
	_, err := ClaimTask(t.Context(), s, "board", "b", "worker", 60)
	require.ErrorContains(t, err, "dependency")
	a, err := ClaimTask(t.Context(), s, "board", "a", "worker", 60)
	require.NoError(t, err)
	require.NoError(t, Update[Task](t.Context(), s, "board", "a", func(task *Task) error { task.LeaseUntil = time.Now().Unix() - 1; return nil }))
	require.Error(t, TransitionTask(t.Context(), s, "board", "a", "submit", a.Attempt, []string{"proof"}, "done"))
	require.NoError(t, TransitionTask(t.Context(), s, "board", "a", "recover", "", nil, ""))
	tasks, err := Tasks(t.Context(), s, "board")
	require.NoError(t, err)
	require.Equal(t, "blocked", tasks[0].Status)
}

func TestJobCoalescesTicksAndDoesNotReplayInterruptedAttempt(t *testing.T) {
	t.Parallel()
	s, root := testStore(t)
	now := time.Now().Unix()
	j := Job{ID: "job", Kind: "cron", Root: root, Prompt: "Check CI", EverySeconds: 60, TimeoutSeconds: 10, MaxRuns: 2, NextAt: now - 600}
	require.NoError(t, SaveJob(t.Context(), s, "jobs", j, 0))
	claimed, err := ClaimJob(t.Context(), s, "jobs", "job", now)
	require.NoError(t, err)
	require.Equal(t, now+60, claimed.NextAt)
	require.Equal(t, 1, claimed.Runs)
	_, err = ClaimJob(t.Context(), s, "jobs", "job", now)
	require.Error(t, err)
	require.NoError(t, Update[Job](t.Context(), s, "jobs", "job", func(j *Job) error { j.LeaseUntil = now - 1; j.NextAt = now - 1; return nil }))
	_, err = ClaimJob(t.Context(), s, "jobs", "job", now)
	require.ErrorContains(t, err, "interrupted")
	require.NoError(t, ControlJob(t.Context(), s, "jobs", "job", "recover"))
	jobs, err := Jobs(t.Context(), s, "jobs")
	require.NoError(t, err)
	require.True(t, jobs[0].Paused)
	require.Equal(t, 1, jobs[0].Runs)
	require.NoError(t, ControlJob(t.Context(), s, "jobs", "job", "resume"))
}

func TestSourceMemoryFreshnessAndScope(t *testing.T) {
	t.Parallel()
	s, root := testStore(t)
	file := filepath.Join(root, "source.go")
	require.NoError(t, os.WriteFile(file, []byte("one"), 0o600))
	m := Memory{ID: "fact", Text: "Uses Go", SessionID: "session"}
	require.NoError(t, SaveMemory(t.Context(), s, "project-a", root, m, []string{"source.go"}))
	entries, err := Memories(t.Context(), s, "project-a", root, "go")
	require.NoError(t, err)
	require.Equal(t, "current", entries[0].Status)
	require.NoError(t, os.WriteFile(file, []byte("two"), 0o600))
	entries, err = Memories(t.Context(), s, "project-a", root, "go")
	require.NoError(t, err)
	require.Equal(t, "stale", entries[0].Status)
	entries, err = Memories(t.Context(), s, "project-b", root, "")
	require.NoError(t, err)
	require.Empty(t, entries)
	_, err = ReadSource(root, "../escape")
	require.Error(t, err)
	require.NoError(t, Update[Memory](t.Context(), s, "project-a", "fact", func(m *Memory) error { m.ValidUntil = time.Now().Unix() - 1; return nil }))
	entries, err = Memories(t.Context(), s, "project-a", root, "")
	require.NoError(t, err)
	require.Equal(t, "expired", entries[0].Status)
}
