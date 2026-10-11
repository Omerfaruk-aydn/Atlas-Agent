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

