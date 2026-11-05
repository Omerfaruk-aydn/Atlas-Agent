package localapps

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateBackupRejectsOverflowingExpandedSize(t *testing.T) {
	t.Parallel()
	archive := filepath.Join(t.TempDir(), "backup.zip")
	f, err := os.Create(archive)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	meta := BackupMeta{Format: backupFormat, AppID: "app", ManifestVersion: ManifestVersion, Files: []BackupFile{{Path: "data/a"}, {Path: "data/b"}}}
	w, err := zw.Create(backupMetaName)
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(w).Encode(meta))
	_, err = zw.CreateRaw(&zip.FileHeader{Name: "data/a", Method: zip.Store, UncompressedSize64: 1})
	require.NoError(t, err)
	_, err = zw.CreateRaw(&zip.FileHeader{Name: "data/b", Method: zip.Store, UncompressedSize64: math.MaxUint64})
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	_, err = (&Manager{}).validateBackup(t.Context(), archive, Record{ID: "app"}, RestoreOptions{})
	require.ErrorContains(t, err, "expands beyond")
}

func TestExtractBackupRejectsSymlinkOutsideDestination(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "data")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "backup.zip")
	f, err := os.Create(archive)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create("data/file.txt")
	require.NoError(t, err)
	_, err = w.Write([]byte("test"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	sum := sha256.Sum256([]byte("test"))
	meta := BackupMeta{Files: []BackupFile{{Path: "data/file.txt", Size: 4, SHA256: hex.EncodeToString(sum[:])}}}
	require.Error(t, extractBackup(t.Context(), archive, root, meta))
	require.NoFileExists(t, filepath.Join(outside, "file.txt"))
}
