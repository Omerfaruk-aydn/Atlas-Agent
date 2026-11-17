package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeManifest(t *testing.T, root, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ManifestFile), []byte(body), 0o600))
}

func TestLoadValidatesManifestAndExecutableContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeManifest(t, root, `{"name":"review-tools","version":"1.0.0","contents":["skills","hooks"],"permissions":["bash:execute"]}`)
	manifest, err := Load(root)
	require.NoError(t, err)
	require.True(t, manifest.Executable)
	for _, invalid := range []string{
		`{"name":"../escape","version":"1","contents":["skills"]}`,
		`{"name":"valid","version":"1","contents":[]}`,
		`{"name":"valid","version":"1","contents":["binaries"]}`,
		`{"name":"valid","version":"1","contents":["skills"],"unknown":true}`,
	} {
		writeManifest(t, root, invalid)
		_, err = Load(root)
		require.Error(t, err, invalid)
	}
}

func TestInstallRequiresExplicitReplacement(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "source")
	writeManifest(t, source, `{"name":"review-tools","version":"1.0.0","contents":["skills"]}`)
	root := t.TempDir()
	_, err := Install(source, root, false)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "review-tools", ManifestFile))
	_, err = Install(source, root, false)
	require.ErrorContains(t, err, "already installed")
	_, err = Install(source, root, true)
	require.NoError(t, err)
}

func TestStateDefaultsAndExplicitChoice(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	root := t.TempDir()

	writeManifest(t, filepath.Join(source, "safe"), `{"name":"safe","version":"1","contents":["skills"]}`)
	writeManifest(t, filepath.Join(source, "runner"), `{"name":"runner","version":"1","contents":["skills","hooks"]}`)
	for _, name := range []string{"safe", "runner"} {
		_, err := Install(filepath.Join(source, name), root, false)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(root, name, "skills"), 0o755))
	}

	// Defaults: code-free packages are on, code-running ones are off.
	require.Equal(t, []string{filepath.Join(root, "safe", "skills")}, ContentDirs(root, "skills"))

	require.NoError(t, SetEnabled(root, "runner", true))
	require.Len(t, ContentDirs(root, "skills"), 2)

	require.NoError(t, SetEnabled(root, "safe", false))
	require.Equal(t, []string{filepath.Join(root, "runner", "skills")}, ContentDirs(root, "skills"))

	require.Error(t, SetEnabled(root, "missing", true))
	require.Error(t, SetEnabled(root, "../escape", true))
}

// A replaced package must not inherit the approval given to the old one.
func TestReinstallForgetsApproval(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "src")
	root := t.TempDir()
	writeManifest(t, source, `{"name":"runner","version":"1","contents":["hooks"]}`)
	_, err := Install(source, root, false)
	require.NoError(t, err)
	require.NoError(t, SetEnabled(root, "runner", true))
	require.True(t, List(root)[0].Enabled)

	_, err = Install(source, root, true)
	require.NoError(t, err)
	require.False(t, List(root)[0].Enabled, "executable package is off again after replacement")

	require.NoError(t, Remove(root, "runner"))
	require.Empty(t, List(root))
	require.NotContains(t, LoadState(root).Enabled, "runner")
}

func TestCorruptStateFileFallsBackToDefaults(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, StateFile), []byte("{not json"), 0o600))
	require.Empty(t, LoadState(root).Enabled)
}
