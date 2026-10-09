package clibridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPinnedExecutableDetectsChangesWithPreservedStat(t *testing.T) {
	t.Parallel()
	path := copyExecutable(t, t.TempDir())
	identity, err := Pin(t.Context(), Executable{Path: path})
	require.NoError(t, err)
	before, err := os.Stat(path)
	require.NoError(t, err)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	var original [1]byte
	_, err = f.ReadAt(original[:], before.Size()-1)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte{original[0] ^ 0xff}, before.Size()-1)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Chtimes(path, before.ModTime(), before.ModTime()))
	_, err = Current(t.Context(), Installed{Pin: &identity})
	require.Equal(t, StatusChanged, StatusOf(err))
	require.ErrorContains(t, err, "re-pin")
	updated, err := Pin(t.Context(), Executable{Path: path})
	require.NoError(t, err)
	require.NotEqual(t, identity.SHA256, updated.SHA256)
	_, err = Current(t.Context(), Installed{Pin: &updated})
	require.NoError(t, err)
}

func TestReportRejectsRecentUnchangedOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "result.out")
	require.NoError(t, os.WriteFile(path, []byte("existing"), 0o600))
	before := time.Now().Add(-200 * time.Millisecond)
	require.NoError(t, os.Chtimes(path, before, before))
	ref, err := resolveOutput(root, Param{Name: "result", Overwrite: true}, "result.out")
	require.NoError(t, err)
	prep := Prepared{Outputs: []FileRef{ref}, Expectations: []OutputExpectation{{Param: "result", MinBytes: 1}}, ExpectedExitCodes: []int{0}}
	report := NewReport("no-write", prep, time.Now())
	report.Finish(prep, 0, "harmless output", "", time.Now())
	require.True(t, report.Observed)
	require.False(t, report.Outputs[0].Changed)
	require.False(t, report.Passed)
	require.Equal(t, OutcomeFailed, report.Outcome)

	// A verified content change passes even when file metadata is preserved.
	require.NoError(t, os.WriteFile(path, []byte("produced"), 0o600))
	require.NoError(t, os.Chtimes(path, before, before))
	report.Finish(prep, 0, "harmless output", "", time.Now())
	require.True(t, report.Passed)

	// Rewriting identical content requires changed modification evidence.
	ref, err = resolveOutput(root, Param{Name: "result", Overwrite: true}, "result.out")
	require.NoError(t, err)
	prep.Outputs = []FileRef{ref}
	report = NewReport("same-content", prep, time.Now())
	now := time.Now()
	require.NoError(t, os.Chtimes(path, now, now))
	report.Finish(prep, 0, "", "", now)
	require.True(t, report.Passed)
}
