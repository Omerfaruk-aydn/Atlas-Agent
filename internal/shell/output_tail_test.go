package shell

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestBackgroundOutputBoundsUTF8AndRejectsOtherProject(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	manager := newBackgroundShellManager()
	job := &BackgroundShell{ID: "job", WorkingDir: root, stdout: &syncBuffer{}, stderr: &syncBuffer{}, done: make(chan struct{})}
	_, err := job.stdout.WriteString(strings.Repeat("界", 20000))
	require.NoError(t, err)
	manager.shells.Set(job.ID, job)
	out, err := manager.OutputForRoot(root, job.ID)
	require.NoError(t, err)
	require.True(t, out.Truncated)
	require.False(t, out.Done)
	require.True(t, utf8.ValidString(out.Stdout))
	require.LessOrEqual(t, len(out.Stdout), 32*1024)
	_, err = manager.OutputForRoot(t.TempDir(), job.ID)
	require.ErrorContains(t, err, "another project")
	close(job.done)
	out, err = manager.OutputForRoot(filepath.Clean(root), job.ID)
	require.NoError(t, err)
	require.True(t, out.Done)
}
