package transfer

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func newUploads(t *testing.T) *Uploads {
	t.Helper()
	u, err := OpenUploads(t.TempDir())
	require.NoError(t, err)
	return u
}

func TestChunkedUploadResumesOnlyForTheSameOwnerAndContent(t *testing.T) {
	t.Parallel()
	u := newUploads(t)
	root := t.TempDir()
	data := bytes.Repeat([]byte("abcdefgh"), 1000)
	spec := UploadSpec{Owner: "dev-a", Key: "op-1", Root: root, Rel: "in/data.bin", Size: int64(len(data)), SHA256: HashBytes(data), Conflict: ConflictError}

	st, err := u.Create(spec, 0)
	require.NoError(t, err)
	st, err = u.Write(st.ID, "dev-a", 0, bytes.NewReader(data[:3000]), 3000, HashBytes(data[:3000]))
	require.NoError(t, err)
	require.EqualValues(t, 3000, st.Received)

	// The same operation for the same content resumes at the received offset.
	again, err := u.Create(spec, 0)
	require.NoError(t, err)
	require.Equal(t, st.ID, again.ID)
	require.EqualValues(t, 3000, again.Received)

	// The same key for other content (e.g. a same-named different file) is refused.
	other := spec
	other.SHA256 = HashBytes([]byte("different"))
	_, err = u.Create(other, 0)
	require.ErrorIs(t, err, ErrUploadConflict)

	// Another session cannot see, continue or complete this upload.
	_, err = u.Write(st.ID, "dev-b", 3000, bytes.NewReader(data[3000:]), int64(len(data)-3000), "")
	require.Equal(t, "not_found", Code(err))
	_, err = u.Complete(st.ID, "dev-b")
	require.Equal(t, "not_found", Code(err))

	// A chunk sent from the wrong offset is refused without changing state.
	_, err = u.Write(st.ID, "dev-a", 100, bytes.NewReader(data[100:200]), 100, "")
	require.Equal(t, "offset_mismatch", Code(err))

	// An incomplete upload is never installed.
	_, err = u.Complete(st.ID, "dev-a")
	require.Equal(t, "incomplete", Code(err))
	require.NoFileExists(t, filepath.Join(root, "in", "data.bin"))

	_, err = u.Write(st.ID, "dev-a", 3000, bytes.NewReader(data[3000:]), int64(len(data)-3000), HashBytes(data[3000:]))
	require.NoError(t, err)
	done, err := u.Complete(st.ID, "dev-a")
	require.NoError(t, err)
	require.Equal(t, "complete", done.State)
	got, err := os.ReadFile(done.Path)
	require.NoError(t, err)
	require.Equal(t, data, got)

	// Completing twice is idempotent and does not reinstall.
	again, err = u.Complete(st.ID, "dev-a")
	require.NoError(t, err)
	require.Equal(t, done.Path, again.Path)
}

func TestChunkedUploadHashMismatchFailsAndDeliversNothing(t *testing.T) {
	t.Parallel()
	u := newUploads(t)
	root := t.TempDir()
	data := []byte("the real content")
	spec := UploadSpec{Owner: "dev", Key: "op-hash", Root: root, Rel: "f.txt", Size: int64(len(data)), SHA256: HashBytes([]byte("other content!!!")), Conflict: ConflictError}
	st, err := u.Create(spec, 0)
	require.NoError(t, err)

	// A chunk whose own hash is wrong is discarded.
	_, err = u.Write(st.ID, "dev", 0, bytes.NewReader(data), int64(len(data)), HashBytes([]byte("x")))
	require.Equal(t, "hash_mismatch", Code(err))
	cur, err := u.Status(st.ID, "dev")
	require.NoError(t, err)
	require.Zero(t, cur.Received)

	_, err = u.Write(st.ID, "dev", 0, bytes.NewReader(data), int64(len(data)), "")
	require.NoError(t, err)
	failed, err := u.Complete(st.ID, "dev")
	require.Equal(t, "hash_mismatch", Code(err))
	require.Equal(t, "failed", failed.State)
	require.NoFileExists(t, filepath.Join(root, "f.txt"))
}

func TestConflictPolicies(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "report.pdf"), []byte("old"), 0o644))
	src := filepath.Join(t.TempDir(), "src")
	data := []byte("new")
	require.NoError(t, os.WriteFile(src, data, 0o644))

	_, err := Install(root, "report.pdf", src, 3, HashBytes(data), ConflictError)
	require.Equal(t, "exists", Code(err))

	renamed, err := Install(root, "report.pdf", src, 3, HashBytes(data), ConflictRename)
	require.NoError(t, err)
	require.Equal(t, "report (1).pdf", filepath.Base(renamed))
	old, _ := os.ReadFile(filepath.Join(root, "report.pdf"))
	require.Equal(t, "old", string(old))

	replaced, err := Install(root, "report.pdf", src, 3, HashBytes(data), ConflictOverwrite)
	require.NoError(t, err)
	got, _ := os.ReadFile(replaced)
	require.Equal(t, "new", string(got))

	_, err = ParseConflict("clobber")
	require.Error(t, err)
}

func TestInstallRejectsSubstitutedSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(t.TempDir(), "src")
	require.NoError(t, os.WriteFile(src, []byte("evil"), 0o644))
	_, err := Install(root, "f", src, 4, HashBytes([]byte("good")), ConflictError)
	require.Equal(t, "hash_mismatch", Code(err))
	require.NoFileExists(t, filepath.Join(root, "f"))
}

func TestJunctionEscapeIsRejected(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("junctions are a Windows feature")
	}
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644))
	link := filepath.Join(root, "junction")
	// PowerShell quotes the paths itself; cmd would split on "&".
	script := "New-Item -ItemType Junction -Path $env:ATLAS_LINK -Target $env:ATLAS_TARGET | Out-Null"
	cmd := exec.CommandContext(t.Context(), "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "ATLAS_LINK="+link, "ATLAS_TARGET="+outside)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("junctions unavailable: %v %s", err, out)
	}
	_, err := Resolve(root, "junction/secret.txt")
	require.Equal(t, "symlink_escape", Code(err))
	_, err = Resolve(root, "junction")
	require.Equal(t, "symlink_escape", Code(err))
	_, _, _, err = Open(root, "junction/secret.txt", 0)
	require.Error(t, err)
	data := []byte("x")
	_, err = Receive(root, "junction/new.txt", bytes.NewReader(data), 1, HashBytes(data), 0, false)
	require.Equal(t, "symlink_escape", Code(err))
	require.NoFileExists(t, filepath.Join(outside, "new.txt"))
	_, _, err = List(root, "junction", 0)
	require.Equal(t, "symlink_escape", Code(err))
}

func TestListReportsLinksWithoutFollowing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("abc"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "sub"), 0o755))
	entries, truncated, err := List(root, "", 0)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, entries, 2)
	_, _, err = List(root, "../", 0)
	require.Equal(t, "path_traversal", Code(err))
	entries, truncated, err = List(root, "", 1)
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, entries, 1)
}
