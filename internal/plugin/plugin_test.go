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
