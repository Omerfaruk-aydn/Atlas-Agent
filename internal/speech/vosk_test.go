package speech

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVoskSelectionSupportsLegacyLayoutAndNeverFallsBack(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	models, runtimeDir := filepath.Join(root, "models"), filepath.Join(root, "runtime")
	write := func(path string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("fixture"), 0o600))
	}
	for _, name := range []string{"libvosk.dll", "libgcc_s_seh-1.dll", "libstdc++-6.dll", "libwinpthread-1.dll"} {
		write(filepath.Join(runtimeDir, name))
	}
	for _, name := range []string{"final.mdl", "mfcc.conf", "HCLr.fst", "Gr.fst"} {
		write(filepath.Join(models, "vosk-model-small-tr-0.3", name))
	}
	opts := DictationOptions{Language: "tr", ModelDir: models, RuntimeDir: runtimeDir}
	status, path, libraries, err := findVosk(opts)
	require.NoError(t, err)
	require.Equal(t, "tr-TR", status.Language)
	require.Equal(t, "vosk", status.Backend)
	require.Equal(t, filepath.Join(models, "vosk-model-small-tr-0.3"), path)
	require.Equal(t, runtimeDir, libraries)
	opts.Language = "ar"
	status, _, _, err = findVosk(opts)
	require.ErrorIs(t, err, ErrNoRecognizer)
	require.Empty(t, status.Language)
	require.Equal(t, []string{"tr-TR"}, status.Installed)
	opts.Language = "tr-TR"
	require.NoError(t, os.Remove(filepath.Join(runtimeDir, "libvosk.dll")))
	_, _, _, err = findVosk(opts)
	require.ErrorContains(t, err, "missing Vosk runtime")
	require.NoError(t, os.Remove(filepath.Join(models, "vosk-model-small-tr-0.3", "Gr.fst")))
	_, _, _, err = findVosk(opts)
	require.ErrorIs(t, err, ErrNoRecognizer)
	require.ErrorContains(t, err, "language tr-TR")
}
