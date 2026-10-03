package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestTickRetainsConcurrentPauseAndSuppressesUnchangedResults(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	now := time.Now()
	job := Job{ID: "fixture", Root: t.TempDir(), Argv: []string{"fixture"}, PeriodMS: 10000, TimeoutMS: 1000, NextAt: now.UnixMilli()}
	require.NoError(t, Save(t.Context(), store, job, 0))
	runs := 0
	events, err := Tick(t.Context(), store, now, func(ctx context.Context, job Job) (int, string, error) {
		runs++
		record, _, err := store.ReadRecord(ctx, namespace, job.ID)
		require.NoError(t, err)
		job.Paused = true
		require.NoError(t, Save(ctx, store, job, record.Revision))
		return 1, "same failure", nil
	})
	require.NoError(t, err)
	require.Len(t, events, 1)
	jobs, err := List(t.Context(), store)
	require.NoError(t, err)
	require.True(t, jobs[0].Paused)
	require.Zero(t, jobs[0].LeaseUntil)
	record, _, err := store.ReadRecord(t.Context(), namespace, job.ID)
	require.NoError(t, err)
	jobs[0].Paused = false
	jobs[0].NextAt = now.UnixMilli()
	require.NoError(t, Save(t.Context(), store, jobs[0], record.Revision))
	events, err = Tick(t.Context(), store, now, func(context.Context, Job) (int, string, error) { runs++; return 1, "same failure", nil })
	require.NoError(t, err)
	require.Empty(t, events)
	require.Equal(t, 2, runs)
}

func TestTickDoesNotReplayInterruptedRun(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	now := time.Now()
	job := Job{ID: "interrupted", Root: t.TempDir(), Argv: []string{"fixture"}, PeriodMS: 10000, TimeoutMS: 1000, NextAt: now.Add(-time.Minute).UnixMilli(), LeaseUntil: now.Add(-time.Second).UnixMilli()}
	require.NoError(t, Save(t.Context(), store, job, 0))
	events, err := Tick(t.Context(), store, now, func(context.Context, Job) (int, string, error) {
		t.Fatal("An interrupted command must not be replayed")
		return 0, "", nil
	})
	require.NoError(t, err)
	require.Empty(t, events)
	jobs, err := List(t.Context(), store)
	require.NoError(t, err)
	require.True(t, jobs[0].Paused)
	require.Equal(t, job.LeaseUntil, jobs[0].LeaseUntil)
}
