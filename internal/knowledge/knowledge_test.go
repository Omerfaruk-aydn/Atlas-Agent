package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProposeApproveIndexAndStaleness(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "build.go"), []byte("v1"), 0o644))
	s := New(filepath.Join(t.TempDir(), "knowledge"), root)

	require.Empty(t, s.Index())
	it, err := s.Propose("How the build works", "go build then embed assets", "Run `go build .`; assets are embedded.", []string{"build.go", "missing.go"})
	require.NoError(t, err)
	require.Equal(t, "how-the-build-works", it.ID)
	require.Equal(t, Proposed, it.Status)
	require.Empty(t, s.Index(), "a proposal is not injected until approved")

	require.NoError(t, s.Approve(it.ID))
	require.Contains(t, s.Index(), "how-the-build-works: How the build works -- go build then embed assets")
	require.NotContains(t, s.Index(), "STALE")
	require.NotContains(t, s.Index(), "assets are embedded", "the body stays out of the index")

	require.NoError(t, os.WriteFile(filepath.Join(root, "build.go"), []byte("v2"), 0o644))
	require.Contains(t, s.Index(), "[STALE: build.go changed")
	require.NotContains(t, s.Index(), "missing.go", "a source that never existed is not stale")

	require.NoError(t, s.Remove(it.ID))
	require.Empty(t, s.Index())
	require.Error(t, s.Remove(it.ID))
}

func TestProposeValidation(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), t.TempDir())
	for _, c := range [][3]string{{"", "s", "b"}, {"t", "", "b"}, {"t", "s", ""}} {
		_, err := s.Propose(c[0], c[1], c[2], nil)
		require.Error(t, err)
	}
	_, err := s.Propose("t", "s", "b", []string{"../escape"})
	require.ErrorContains(t, err, "inside the project")
	_, err = s.Propose("Same", "s", "b", nil)
	require.NoError(t, err)
	_, err = s.Propose("Same", "s", "b", nil)
	require.ErrorContains(t, err, "already exists")
}

func TestProposeCheckpointFromACompactionSummary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "server.go"), []byte("x"), 0o644))
	s := New(filepath.Join(t.TempDir(), "knowledge"), root)
	now := time.Date(2026, 11, 17, 23, 0, 0, 0, time.UTC)

	_, why, err := s.ProposeCheckpoint("short", now)
	require.NoError(t, err)
	require.Contains(t, why, "too short")

	_, why, err = s.ProposeCheckpoint("We configured the deploy. key: sk-abcdefghijklmnopqrstuvwxyz123456 "+strings.Repeat("x", 200), now)
	require.NoError(t, err)
	require.Contains(t, why, "credential")

	summary := "## Goal\nMake the server reject expired tokens.\n\nWe changed server.go and found that missing.go does not exist.\n" + strings.Repeat("Detail about the work. ", 20)
	it, why, err := s.ProposeCheckpoint(summary, now)
	require.NoError(t, err)
	require.Empty(t, why)
	require.Equal(t, "checkpoint-2026-11-17-23-00", it.ID)
	require.Equal(t, Proposed, it.Status)
	require.Equal(t, "Goal", it.Summary)
	require.Len(t, it.Sources, 1)
	require.Equal(t, "server.go", it.Sources[0].Path)
	require.Empty(t, s.Index(), "an automatic proposal is not context until approved")

	// Too many waiting proposals stop further automatic ones.
	for i := range 9 {
		_, _, err := s.ProposeCheckpoint(summary, now.Add(time.Duration(i+1)*time.Minute))
		require.NoError(t, err)
	}
	_, why, err = s.ProposeCheckpoint(summary, now.Add(time.Hour))
	require.NoError(t, err)
	require.Contains(t, why, "waiting for review")
}
