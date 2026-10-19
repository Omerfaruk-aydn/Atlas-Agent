package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A job read while it runs returns only what is new, the note names the log
// with the whole output, and the log matches what the job printed.
func TestJobOutputReadsReturnOnlyNewOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	id := "job-" + t.Name()
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	first := readJobOutput(root, "s", id, started, "line 1\nline 2\n", "", false)
	require.Equal(t, "line 1\nline 2\n", first.stdout)
	require.Empty(t, first.note)

	second := readJobOutput(root, "s", id, started, "line 1\nline 2\nline 3\n", "warn\n", false)
	require.Equal(t, "line 3\n", second.stdout)
	require.Equal(t, "warn\n", second.stderr)
	require.Contains(t, second.note, "The 2 lines before it were returned earlier")
	rel := second.note[strings.Index(second.note, ".atlas/logs/") : len(second.note)-2]
	saved, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)
	require.Equal(t, "line 1\nline 2\nline 3\n\n--- stderr ---\nwarn\n", string(saved))

	idle := readJobOutput(root, "s", id, started, "line 1\nline 2\nline 3\n", "warn\n", false)
	require.Empty(t, idle.stdout)
	require.Empty(t, idle.stderr)
	require.Contains(t, idle.note, "No new output since your last read")

	// The read that sees the job finish returns what is new; the read
	// after that returns everything again.
	last := readJobOutput(root, "s", id, started, "line 1\nline 2\nline 3\ndone\n", "warn\n", true)
	require.Equal(t, "done\n", last.stdout)
	again := readJobOutput(root, "s", id, started, "line 1\nline 2\nline 3\ndone\n", "warn\n", true)
	require.Equal(t, "line 1\nline 2\nline 3\ndone\n", again.stdout)
	require.Empty(t, again.note)
}

// Without a project directory, or when the log cannot be written, every
// read returns the whole output: losing the log must never lose output.
func TestJobOutputWithoutLogReturnsEverything(t *testing.T) {
	t.Parallel()
	started := time.Now()
	id := "job-" + t.Name()
	readJobOutput("", "s", id, started, "a\n", "", false)
	require.Equal(t, "a\nb\n", readJobOutput("", "s", id, started, "a\nb\n", "", false).stdout)

	blocked := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocked, nil, 0o644))
	id2 := id + "-blocked"
	readJobOutput(blocked, "s", id2, started, "a\n", "", false)
	view := readJobOutput(blocked, "s", id2, started, "a\nb\n", "", false)
	require.Equal(t, "a\nb\n", view.stdout)
	require.Empty(t, view.note)
}

// Each session reads a job from its own cursor: a sub-agent's reads do not
// hide output from the parent.
func TestJobOutputCursorsArePerSession(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	id := "job-" + t.Name()
	started := time.Now()
	readJobOutput(root, "child", id, started, "a\n", "", false)
	readJobOutput(root, "child", id, started, "a\nb\n", "", false)
	parent := readJobOutput(root, "parent", id, started, "a\nb\n", "", false)
	require.Equal(t, "a\nb\n", parent.stdout)
	require.Empty(t, parent.note)
}
