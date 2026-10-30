package routine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.UTC)
	if err != nil {
		panic(err)
	}
	return t
}

func TestScheduleNext(t *testing.T) {
	t.Parallel()
	cases := []struct {
		expr, after, want string
	}{
		{"* * * * *", "2026-03-01 10:00", "2026-03-01 10:01"},
		{"*/15 * * * *", "2026-03-01 10:07", "2026-03-01 10:15"},
		{"0 9 * * *", "2026-03-01 09:00", "2026-03-02 09:00"},
		{"30 8 * * mon-fri", "2026-03-06 09:00", "2026-03-09 08:30"}, // Friday to Monday.
		{"0 0 1 * *", "2026-01-31 12:00", "2026-02-01 00:00"},
		{"0 12 29 2 *", "2026-03-01 00:00", "2028-02-29 12:00"},
		{"0 0 * * 7", "2026-03-01 00:00", "2026-03-08 00:00"}, // 7 is Sunday; 2026-03-01 is one.
		{"15,45 */6 * * *", "2026-03-01 06:15", "2026-03-01 06:45"},
		{"@hourly", "2026-03-01 10:30", "2026-03-01 11:00"},
		{"@weekly", "2026-03-01 10:30", "2026-03-08 00:00"},
		{"5-10/2 0 * jan,mar *", "2026-02-01 00:00", "2026-03-01 00:05"},
		// Both day fields restricted: either may match.
		{"0 0 13 * fri", "2026-03-01 00:00", "2026-03-06 00:00"},
	}
	for _, tc := range cases {
		t.Run(tc.expr+"@"+tc.after, func(t *testing.T) {
			t.Parallel()
			s, err := ParseSchedule(tc.expr)
			require.NoError(t, err)
			require.Equal(t, at(tc.want), s.Next(at(tc.after)))
		})
	}
}

func TestScheduleNextIsStrictlyAfter(t *testing.T) {
	t.Parallel()
	s, err := ParseSchedule("0 9 * * *")
	require.NoError(t, err)
	require.Equal(t, at("2026-03-02 09:00"), s.Next(at("2026-03-01 09:00")))
	require.Equal(t, at("2026-03-01 09:00"), s.Next(at("2026-03-01 08:59")))
}

func TestScheduleImpossibleDateReturnsZero(t *testing.T) {
	t.Parallel()
	s, err := ParseSchedule("0 0 31 2 *")
	require.NoError(t, err)
	require.True(t, s.Next(at("2026-01-01 00:00")).IsZero())
}

func TestParseScheduleRejectsBadInput(t *testing.T) {
	t.Parallel()
	for _, expr := range []string{
		"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *",
		"* * * 13 *", "* * * * 8", "5-1 * * * *", "*/0 * * * *", "a * * * *",
		"@nonsense", ",1 * * * *", "1,,2 * * * *",
	} {
		_, err := ParseSchedule(expr)
		require.Error(t, err, expr)
	}
}

func newTestStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	s := NewStore(t.TempDir())
	now := at("2026-03-02 08:00")
	s.now = func() time.Time { return now }
	return s, &now
}

func sample(name string) Routine {
	return Routine{
		Name: name, Schedule: "0 9 * * *", Prompt: "do the thing",
		Dir: filepath.Join(os.TempDir(), "x"), Permissions: PermissionsConfigured, Enabled: true,
	}
}

func TestValidateDefaultsAndRejections(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()

	r, err := s.Add(ctx, Routine{Schedule: "@daily", Prompt: "p", Dir: sample("").Dir, Enabled: true})
	require.NoError(t, err)
	require.Equal(t, PermissionsConfigured, r.Permissions, "the default must not be auto")
	require.True(t, strings.HasPrefix(r.ID, "rt-"))
	require.NotEmpty(t, r.Name)

	bad := []Routine{
		{Name: "bad name", Schedule: "@daily", Prompt: "p", Dir: sample("").Dir},
		{Name: "a", Schedule: "nope", Prompt: "p", Dir: sample("").Dir},
		{Name: "b", Schedule: "@daily", Prompt: "  ", Dir: sample("").Dir},
		{Name: "c", Schedule: "@daily", Prompt: "p", Dir: "relative"},
		{Name: "d", Schedule: "@daily", Prompt: "p", Dir: sample("").Dir, Permissions: "bypass"},
		{Name: "e", Schedule: "@daily", Prompt: "p", Dir: sample("").Dir, TimeoutSeconds: -1},
	}
	for _, b := range bad {
		_, err := s.Add(ctx, b)
		require.Error(t, err, b.Name)
	}
}

func TestStoreCRUD(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()

	a, err := s.Add(ctx, sample("alpha"))
	require.NoError(t, err)
	_, err = s.Add(ctx, sample("alpha"))
	require.ErrorContains(t, err, "already exists")
	_, err = s.Add(ctx, sample("beta"))
	require.NoError(t, err)

	list, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)

	got, err := s.Get(ctx, "alpha")
	require.NoError(t, err)
	require.Equal(t, a.ID, got.ID)
	got, err = s.Get(ctx, a.ID[:6])
	require.NoError(t, err)
	require.Equal(t, a.ID, got.ID)

	paused, err := s.SetEnabled(ctx, "alpha", false)
	require.NoError(t, err)
	require.False(t, paused.Enabled)

	_, err = s.Remove(ctx, "alpha")
	require.NoError(t, err)
	_, err = s.Get(ctx, "alpha")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = s.Remove(ctx, "alpha")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestResumeDoesNotCatchUpWhilePaused(t *testing.T) {
	t.Parallel()
	s, now := newTestStore(t)
	ctx := context.Background()
	_, err := s.Add(ctx, sample("p"))
	require.NoError(t, err)
	_, err = s.SetEnabled(ctx, "p", false)
	require.NoError(t, err)

	*now = at("2026-03-05 12:00")
	r, err := s.SetEnabled(ctx, "p", true)
	require.NoError(t, err)
	_, due := r.DueSlot(*now)
	require.False(t, due, "days of paused time must not trigger a run")
}

func TestDueSlotCollapsesMissedOccurrences(t *testing.T) {
	t.Parallel()
	r := sample("x")
	r.CreatedAt = at("2026-03-01 00:00")
	slot, due := r.DueSlot(at("2026-03-05 12:00"))
	require.True(t, due)
	require.Equal(t, at("2026-03-05 09:00"), slot, "the newest missed occurrence stands for all of them")

	r.LastSlot = slot
	_, due = r.DueSlot(at("2026-03-05 12:00"))
	require.False(t, due)

	r.Enabled = false
	_, due = r.DueSlot(at("2026-03-05 12:00"))
	require.False(t, due)
}

func TestClaimBlocksOverlapUntilLeaseLapses(t *testing.T) {
	t.Parallel()
	s, now := newTestStore(t)
	ctx := context.Background()
	r, err := s.Add(ctx, sample("c"))
	require.NoError(t, err)

	_, err = s.Claim(ctx, r.ID, time.Time{})
	require.NoError(t, err)
	_, err = s.Claim(ctx, r.ID, time.Time{})
	require.ErrorIs(t, err, ErrBusy)

	*now = now.Add(DefaultTimeout + 2*time.Minute)
	_, err = s.Claim(ctx, r.ID, time.Time{})
	require.NoError(t, err, "a crashed holder's lease must expire")
}

func TestLogsNewestFirstAndBounded(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	r, err := s.Add(ctx, sample("l"))
	require.NoError(t, err)

	for i := range maxLogEntries + 5 {
		require.NoError(t, s.Finish(ctx, LogEntry{
			RoutineID: r.ID, RoutineName: r.Name, Status: StatusSucceeded,
			StartedAt: at("2026-03-02 09:00").Add(time.Duration(i) * time.Minute),
			Output:    strings.Repeat("x", 10_000),
		}))
	}
	all, err := s.Logs(ctx, "l", 0)
	require.NoError(t, err)
	require.LessOrEqual(t, len(all), maxLogEntries)
	require.True(t, all[0].StartedAt.After(all[1].StartedAt))
	require.Equal(t, at("2026-03-02 09:00").Add(time.Duration(maxLogEntries+4)*time.Minute), all[0].StartedAt, "the newest run is kept")
	info, err := os.Stat(s.logPath(r.ID))
	require.NoError(t, err)
	require.Less(t, info.Size(), int64(logCompactBytes+20_000), "the log file stays bounded")

	few, err := s.Logs(ctx, "l", 3)
	require.NoError(t, err)
	require.Len(t, few, 3)
}

func TestTruncateOutputKeepsTail(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", maxOutputBytes) + "THE END"
	got := truncateOutput(long)
	require.True(t, strings.HasSuffix(got, "THE END"))
	require.True(t, strings.HasPrefix(got, "[output truncated]"))
	require.Equal(t, "short", truncateOutput("short"))
}

func TestStoreSurvivesConcurrentWriters(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			_, err := s.Add(ctx, sample("r"+string(rune('a'+i))))
			require.NoError(t, err)
		})
	}
	wg.Wait()
	list, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 8)
}

func newRunner(t *testing.T, exec Executor) (*Runner, *time.Time) {
	t.Helper()
	s, now := newTestStore(t)
	return &Runner{Store: s, Exec: exec, Now: func() time.Time { return *now }}, now
}

func TestRunDueRunsOnlyDueEnabledRoutines(t *testing.T) {
	t.Parallel()
	var ran sync.Map
	rn, now := newRunner(t, ExecutorFunc(func(_ context.Context, r Routine) (Result, error) {
		ran.Store(r.Name, true)
		return Result{Output: "done " + r.Name, SessionID: "s-" + r.Name, Cost: 0.5}, nil
	}))
	ctx := context.Background()
	_, err := rn.Store.Add(ctx, sample("due"))
	require.NoError(t, err)
	paused := sample("paused")
	paused.Enabled = false
	_, err = rn.Store.Add(ctx, paused)
	require.NoError(t, err)
	later := sample("later")
	later.Schedule = "0 23 * * *"
	_, err = rn.Store.Add(ctx, later)
	require.NoError(t, err)

	*now = at("2026-03-02 09:00")
	entries, err := rn.RunDue(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "due", entries[0].RoutineName)
	require.Equal(t, StatusSucceeded, entries[0].Status)
	require.Equal(t, TriggerSchedule, entries[0].Trigger)
	require.Equal(t, "s-due", entries[0].SessionID)
	_, ok := ran.Load("paused")
	require.False(t, ok)

	// The same occurrence never runs twice.
	entries, err = rn.RunDue(ctx)
	require.NoError(t, err)
	require.Empty(t, entries)

	logs, err := rn.Store.Logs(ctx, "due", 5)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "done due", logs[0].Output)

	got, err := rn.Store.Get(ctx, "due")
	require.NoError(t, err)
	require.Equal(t, StatusSucceeded, got.LastStatus)
	require.False(t, got.Running(*now), "the lease must be released")
}

func TestRunDueRunsOnceAfterDowntime(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	rn, now := newRunner(t, ExecutorFunc(func(context.Context, Routine) (Result, error) {
		calls.Add(1)
		return Result{}, nil
	}))
	ctx := context.Background()
	_, err := rn.Store.Add(ctx, sample("daily"))
	require.NoError(t, err)

	*now = at("2026-03-06 12:00") // Four 09:00 slots passed.
	_, err = rn.RunDue(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
	*now = at("2026-03-06 12:01")
	_, err = rn.RunDue(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "a backlog must not replay")
	*now = at("2026-03-07 09:00")
	_, err = rn.RunDue(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
}

func TestRunDueSkipsLateOccurrences(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	rn, now := newRunner(t, ExecutorFunc(func(context.Context, Routine) (Result, error) {
		calls.Add(1)
		return Result{}, nil
	}))
	ctx := context.Background()
	r := sample("late")
	r.MaxLateSeconds = 600
	_, err := rn.Store.Add(ctx, r)
	require.NoError(t, err)

	*now = at("2026-03-02 11:00")
	entries, err := rn.RunDue(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, StatusSkipped, entries[0].Status)
	require.Zero(t, calls.Load())
}

func TestRunRecordsFailureAndTimeout(t *testing.T) {
	t.Parallel()
	rn, now := newRunner(t, ExecutorFunc(func(ctx context.Context, r Routine) (Result, error) {
		if r.Name == "slow" {
			<-ctx.Done()
			return Result{Output: "partial"}, ctx.Err()
		}
		return Result{Output: "boom output"}, errors.New("exit status 1")
	}))
	ctx := context.Background()
	slow := sample("slow")
	slow.TimeoutSeconds = 1
	_, err := rn.Store.Add(ctx, slow)
	require.NoError(t, err)
	_, err = rn.Store.Add(ctx, sample("broken"))
	require.NoError(t, err)

	*now = at("2026-03-02 09:00")
	entries, err := rn.RunDue(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	byName := map[string]LogEntry{}
	for _, e := range entries {
		byName[e.RoutineName] = e
	}
	require.Equal(t, StatusTimedOut, byName["slow"].Status)
	require.Equal(t, "partial", byName["slow"].Output)
	require.Equal(t, StatusFailed, byName["broken"].Status)
	require.Contains(t, byName["broken"].Error, "exit status 1")
}

func TestRunOneIgnoresScheduleAndDoesNotMoveIt(t *testing.T) {
	t.Parallel()
	rn, now := newRunner(t, ExecutorFunc(func(context.Context, Routine) (Result, error) {
		return Result{Output: "ok"}, nil
	}))
	ctx := context.Background()
	disabled := sample("manual")
	disabled.Enabled = false
	_, err := rn.Store.Add(ctx, disabled)
	require.NoError(t, err)

	e, err := rn.RunOne(ctx, "manual")
	require.NoError(t, err)
	require.Equal(t, TriggerManual, e.Trigger)
	got, err := rn.Store.Get(ctx, "manual")
	require.NoError(t, err)
	require.True(t, got.LastSlot.IsZero())

	_, err = rn.RunOne(ctx, "missing")
	require.ErrorIs(t, err, ErrNotFound)
	_ = now
}

func TestRunOneRefusesOverlap(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	started := make(chan struct{})
	rn, _ := newRunner(t, ExecutorFunc(func(context.Context, Routine) (Result, error) {
		close(started)
		<-release
		return Result{}, nil
	}))
	ctx := context.Background()
	_, err := rn.Store.Add(ctx, sample("busy"))
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { _, err := rn.RunOne(ctx, "busy"); done <- err }()
	<-started
	_, err = rn.RunOne(ctx, "busy")
	require.ErrorIs(t, err, ErrBusy)
	close(release)
	require.NoError(t, <-done)
}

func TestConcurrencyLimit(t *testing.T) {
	t.Parallel()
	var running, peak atomic.Int32
	rn, now := newRunner(t, ExecutorFunc(func(context.Context, Routine) (Result, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		running.Add(-1)
		return Result{}, nil
	}))
	rn.Concurrency = 2
	ctx := context.Background()
	for _, n := range []string{"a", "b", "c", "d"} {
		_, err := rn.Store.Add(ctx, sample(n))
		require.NoError(t, err)
	}
	*now = at("2026-03-02 09:00")
	entries, err := rn.RunDue(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 4)
	require.LessOrEqual(t, peak.Load(), int32(2))
}

func TestDaemonRunsDueRoutinesAndStops(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	rn, now := newRunner(t, ExecutorFunc(func(context.Context, Routine) (Result, error) {
		calls.Add(1)
		return Result{}, nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := rn.Store.Add(ctx, sample("d"))
	require.NoError(t, err)
	*now = at("2026-03-02 09:00")

	seen := make(chan LogEntry, 4)
	done := make(chan error, 1)
	go func() {
		done <- rn.Daemon(ctx, DaemonOptions{Interval: 10 * time.Millisecond, OnRun: func(e LogEntry) { seen <- e }})
	}()
	select {
	case e := <-seen:
		require.Equal(t, "d", e.RoutineName)
	case <-time.After(60 * time.Second):
		t.Fatal("daemon never ran the due routine")
	}

	// A second daemon on the same store is refused.
	other := &Runner{Store: rn.Store, Exec: rn.Exec}
	require.ErrorIs(t, other.Daemon(ctx, DaemonOptions{Interval: time.Hour}), ErrDaemonRunning)

	cancel()
	require.NoError(t, <-done)
	require.EqualValues(t, 1, calls.Load(), "one occurrence runs once however many ticks pass")
}

func TestProcessExecutorArgs(t *testing.T) {
	t.Parallel()
	e := ProcessExecutor{ExtraArgs: []string{"--data-dir", "/d"}}
	args := e.Args(Routine{Dir: "/w", Permissions: PermissionsReadOnly, Model: "p/m", Goal: true})
	require.Equal(t, []string{
		"--cwd", "/w", "--data-dir", "/d", "run", "--json", "--quiet",
		"--permissions", "readonly", "--model", "p/m", "--goal",
	}, args)
	for _, a := range e.Args(Routine{Dir: "/w", Permissions: PermissionsConfigured}) {
		require.NotEqual(t, "--yolo", a)
		require.NotEqual(t, "-y", a)
	}
}

func TestParseRunOutput(t *testing.T) {
	t.Parallel()
	res := parseRunOutput([]byte(`{"session_id":"abc","response":"hello","cost":0.25}`))
	require.Equal(t, Result{Output: "hello", SessionID: "abc", Cost: 0.25}, res)
	require.Equal(t, "plain text", parseRunOutput([]byte("plain text\n")).Output)
}
