package harnessmetrics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPruneTracesRemovesOldInactiveSessionsOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := New(func() string { return root })
	r.BeginTrace("old")
	r.BeginTrace("active")
	old := tracePath(root, "old")
	now := time.Now()
	require.NoError(t, os.Chtimes(old, now.Add(-31*24*time.Hour), now.Add(-31*24*time.Hour)))
	keep := filepath.Join(filepath.Dir(old), "user-notes.jsonl")
	require.NoError(t, os.WriteFile(keep, []byte("keep"), 0o600))
	n, err := PruneTraces(context.Background(), root, now, 30*24*time.Hour, 512<<20)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NoFileExists(t, old)
	require.FileExists(t, tracePath(root, "active"))
	require.FileExists(t, keep)
}

func TestTraceQuotaPreservesRecentSessionsAndCancellation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := New(func() string { return root })
	r.BeginTrace("inactive")
	r.BeginTrace("active")
	now := time.Now()
	old := tracePath(root, "inactive")
	require.NoError(t, os.Chtimes(old, now.Add(-48*time.Hour), now.Add(-48*time.Hour)))
	n, err := PruneTraces(t.Context(), root, now, 30*24*time.Hour, 1)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.FileExists(t, tracePath(root, "active"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = PruneTraces(ctx, root, now, 30*24*time.Hour, 1)
	require.ErrorIs(t, err, context.Canceled)
}
