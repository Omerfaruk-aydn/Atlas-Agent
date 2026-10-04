package speech

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func speechArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, content := range entries {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return b.Bytes()
}

func TestInstallSpeechArchiveVerifiesHashAndReusesInstalledFiles(t *testing.T) {
	archive := speechArchive(t, map[string]string{"model/final.mdl": "model"})
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++; _, _ = w.Write(archive) }))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "model")
	asset := speechAsset{URL: server.URL, SHA256: fmt.Sprintf("%x", sha256.Sum256(archive)), Folder: "model", Required: []string{"final.mdl"}}
	require.NoError(t, installSpeechAsset(t.Context(), server.Client(), asset, dest))
	data, err := os.ReadFile(filepath.Join(dest, "final.mdl"))
	require.NoError(t, err)
	require.Equal(t, "model", string(data))
	require.NoError(t, installSpeechAsset(t.Context(), server.Client(), asset, dest))
	require.Equal(t, 1, requests)
}

func TestInstallSpeechArchiveRejectsHashMismatchAndPathTraversal(t *testing.T) {
	for _, traversal := range []bool{false, true} {
		entries := map[string]string{"model/final.mdl": "model"}
		if traversal {
			entries["model/../escaped.txt"] = "escaped"
		}
		archive := speechArchive(t, entries)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) }))
		asset := speechAsset{URL: server.URL, SHA256: "wrong", Folder: "model", Required: []string{"final.mdl"}}
		if traversal {
			asset.SHA256 = fmt.Sprintf("%x", sha256.Sum256(archive))
		}
		parent := t.TempDir()
		dest := filepath.Join(parent, "model")
		require.Error(t, installSpeechAsset(t.Context(), server.Client(), asset, dest))
		require.NoFileExists(t, filepath.Join(parent, "escaped.txt"))
		require.NoDirExists(t, dest)
		server.Close()
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, installSpeechAsset(ctx, http.DefaultClient, speechAsset{URL: "https://example.invalid"}, filepath.Join(t.TempDir(), "model")), context.Canceled)
}
