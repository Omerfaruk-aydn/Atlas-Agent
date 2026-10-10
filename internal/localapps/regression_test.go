package localapps

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type regressionRuntime struct{}

func (regressionRuntime) CheckRuntime(context.Context, string) RuntimeStatus {
	return RuntimeStatus{Kind: RuntimePython, Available: true, Version: "3.12"}
}

func plannedRegressionApp(t *testing.T) (*Manager, Record) {
	t.Helper()
	m := NewManager(Options{StateDir: t.TempDir(), AppsHome: t.TempDir(), Runtime: regressionRuntime{}})
	r, err := m.Plan(t.Context(), PlanInput{Name: "Regression app", Spec: Spec{
		Purpose: "Store records", Entities: []Entity{{Name: "items", Fields: []Field{{Name: "name", Type: "text"}}}}, Acceptance: []string{"Records persist"},
	}})
	require.NoError(t, err)
	_, err = m.Scaffold(t.Context(), r.ID)
	require.NoError(t, err)
	return m, r
}

func writeRegressionFile(t *testing.T, path, value string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(value), 0o644))
}

func TestSourceRestorePreservesExcludedDirectories(t *testing.T) {
	t.Parallel()
	m, rec := plannedRegressionApp(t)
	preserved := []string{".git/config", ".venv/pyvenv.cfg", "static/.git/config", "static/venv/config", ".atlas-private/state"}
	for _, rel := range preserved {
		writeRegressionFile(t, filepath.Join(rec.Root, rel), "keep "+rel)
	}
	backup, err := m.Backup(t.Context(), rec.ID, "regression", true)
	require.NoError(t, err)
	writeRegressionFile(t, filepath.Join(rec.Root, "server.py"), "changed")
	writeRegressionFile(t, filepath.Join(rec.Root, "static/obsolete.js"), "remove")
	writeRegressionFile(t, filepath.Join(rec.Root, "data/extra.txt"), "changed data")
	_, err = m.Restore(t.Context(), rec.ID, backup.ID, RestoreOptions{Source: true})
	require.NoError(t, err)
	for _, rel := range preserved {
		data, err := os.ReadFile(filepath.Join(rec.Root, rel))
		require.NoError(t, err)
		require.Equal(t, "keep "+rel, string(data))
	}
	require.NoFileExists(t, filepath.Join(rec.Root, "static/obsolete.js"))
	data, err := os.ReadFile(filepath.Join(rec.Root, "server.py"))
	require.NoError(t, err)
	require.NotEqual(t, "changed", string(data))
}

func TestSourceRestoreConflictRestoresOriginalFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	staging := filepath.Join(root, ".atlas-restore-test")
	restored := filepath.Join(staging, "source")
	writeRegressionFile(t, filepath.Join(root, "old.txt"), "original")
	writeRegressionFile(t, filepath.Join(root, "static/.git/config"), "history")
	writeRegressionFile(t, filepath.Join(restored, ManifestFile), "{}")
	writeRegressionFile(t, filepath.Join(restored, "static"), "cannot replace protected tree")
	require.ErrorIs(t, swapSource(root, restored, staging, []string{"data"}), ErrUnsafePath)
	require.NoError(t, os.RemoveAll(staging))
	data, err := os.ReadFile(filepath.Join(root, "old.txt"))
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
	require.FileExists(t, filepath.Join(root, "static/.git/config"))
	require.NoFileExists(t, filepath.Join(root, ManifestFile))
}

func TestSourceHashDetectsLargeFileTail(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "large.js")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Truncate((32<<20)+1))
	require.NoError(t, f.Close())
	before, err := sourceHash(root, []string{"data"})
	require.NoError(t, err)
	f, err = os.OpenFile(path, os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte("x"), 32<<20)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	after, err := sourceHash(root, []string{"data"})
	require.NoError(t, err)
	require.NotEqual(t, before, after)
}

func TestSourceHashIncludesFilesBeyondLegacyLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for i := range 5003 {
		writeRegressionFile(t, filepath.Join(root, strconv.Itoa(10000+i)+".txt"), "original")
	}
	before, err := sourceHash(root, nil)
	require.NoError(t, err)
	writeRegressionFile(t, filepath.Join(root, "15002.txt"), "changed")
	after, err := sourceHash(root, nil)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
}

func TestSourceHashFailureDoesNotReturnPartialFingerprint(t *testing.T) {
	t.Parallel()
	hash, err := sourceHash(filepath.Join(t.TempDir(), "missing"), nil)
	require.Error(t, err)
	require.Empty(t, hash)
}

func TestAppListAndGetInvalidateChangedVerification(t *testing.T) {
	t.Parallel()
	m, rec := plannedRegressionApp(t)
	man, err := LoadManifest(rec.Root)
	require.NoError(t, err)
	hash, err := sourceHash(rec.Root, man.DataDirs)
	require.NoError(t, err)
	_, err = m.mutate(t.Context(), rec.ID, func(r *Record) error {
		r.State = StateReady
		r.LastVerification = &Verification{Passed: true, SourceHash: hash}
		r.Acceptance = []Acceptance{{Index: 1, Passed: true, SourceHash: hash}}
		return nil
	})
	require.NoError(t, err)
	app, err := m.Get(t.Context(), rec.ID)
	require.NoError(t, err)
	require.Equal(t, StateReady, app.State)
	writeRegressionFile(t, filepath.Join(rec.Root, "server.py"), "changed source")
	app, err = m.Get(t.Context(), rec.ID)
	require.NoError(t, err)
	require.True(t, app.SourceChanged)
	require.Equal(t, StateScaffolded, app.State)
	require.Len(t, app.PendingAcceptance, 1)
	apps, err := m.List(t.Context())
	require.NoError(t, err)
	require.True(t, apps[0].SourceChanged)
	require.Equal(t, StateScaffolded, apps[0].State)
}
