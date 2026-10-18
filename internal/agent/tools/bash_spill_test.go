package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func noisyOutput(lines int, failAt int) string {
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		if i == failAt {
			fmt.Fprintf(&b, "--- FAIL: TestCheckout (0.01s) cart.go:42 total = 90, want 100\n")
			continue
		}
		fmt.Fprintf(&b, "ok   step %d compiled package and ran a long uneventful line of output\n", i)
	}
	return b.String()
}

func TestSpillKeepsSmallOutputInline(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	out, errOut := spillOutput(root, "short", "warn")
	require.Equal(t, "short", out)
	require.Equal(t, "warn", errOut)
	_, err := os.Stat(filepath.Join(root, ".atlas", "logs"))
	require.True(t, os.IsNotExist(err))
}

func TestSpillSavesFullOutputAndShowsTheFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	full := noisyOutput(2000, 1200)
	out, errOut := spillOutput(root, full, "")
	require.Empty(t, errOut)
	require.Contains(t, out, SpilledOutputMarker+".atlas/logs/cmd-")
	require.Contains(t, out, "--- FAIL: TestCheckout", "the failure from the middle survives")
	require.Contains(t, out, "1200| --- FAIL", "with its line number in the log")
	require.Contains(t, out, "step 1 compiled", "head is kept")
	require.Contains(t, out, "step 2000 compiled", "tail is kept")
	require.Less(t, len(out), len(full)/4)

	logs, err := filepath.Glob(filepath.Join(root, ".atlas", "logs", "cmd-*.log"))
	require.NoError(t, err)
	require.Len(t, logs, 1)
	saved, err := os.ReadFile(logs[0])
	require.NoError(t, err)
	require.Equal(t, full, string(saved), "the log holds the complete output")
}

func TestSpillKeepsStderrSeparate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	out, errOut := spillOutput(root, noisyOutput(1000, 0), "fatal: compile error in main.go")
	require.Contains(t, out, SpilledOutputMarker)
	require.Contains(t, errOut, "fatal: compile error")
	logs, _ := filepath.Glob(filepath.Join(root, ".atlas", "logs", "cmd-*.log"))
	saved, err := os.ReadFile(logs[0])
	require.NoError(t, err)
	require.Contains(t, string(saved), "--- stderr ---\nfatal: compile error")
}

func TestSpillFallsBackWhenTheLogCannotBeWritten(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// A file where the .atlas directory should be makes the write fail.
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atlas"), []byte("x"), 0o644))
	full := noisyOutput(1000, 0)
	out, _ := spillOutput(root, full, "")
	require.Equal(t, full, out, "the output is never lost")
}

func TestSpillPrunesOldLogs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, ".atlas", "logs")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for i := range spillKeepFiles + 5 {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("cmd-20200101-0000%02d-aaaaaa.log", i)), nil, 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep.txt"), nil, 0o644))
	spillOutput(root, noisyOutput(1000, 0), "")

	logs, _ := filepath.Glob(filepath.Join(dir, "cmd-*.log"))
	require.Len(t, logs, spillKeepFiles)
	_, err := os.Stat(filepath.Join(dir, "cmd-20200101-000000-aaaaaa.log"))
	require.True(t, os.IsNotExist(err), "the oldest go first")
	_, err = os.Stat(filepath.Join(dir, "keep.txt"))
	require.NoError(t, err, "other files are left alone")
}

func TestFormatOutputSpillsBeforeTruncating(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	out := formatOutput(noisyOutput(3000, 1500), "", nil, MaxOutputLength, root)
	require.Contains(t, out, SpilledOutputMarker)
	require.Contains(t, out, "--- FAIL: TestCheckout")
	require.NotContains(t, out, "lines truncated", "the digest fits without blind truncation")
}
