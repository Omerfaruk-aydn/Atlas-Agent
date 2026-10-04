package model

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/stretchr/testify/require"
)

func TestPreparedAttachmentRejectsInvalidImagesDirectoriesAndOversizedFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.png")
	require.NoError(t, os.WriteFile(broken, []byte("\x89PNG\r\n\x1a\n"), 0o600))
	_, err := prepareAttachment(t.Context(), broken)
	require.ErrorContains(t, err, "image header")
	_, err = prepareAttachment(t.Context(), filepath.Join(dir, "missing.png"))
	require.ErrorIs(t, err, os.ErrNotExist)
	folder := filepath.Join(dir, "album.png")
	require.NoError(t, os.Mkdir(folder, 0o750))
	_, err = prepareAttachment(t.Context(), folder)
	require.ErrorContains(t, err, "regular files")
	large := filepath.Join(dir, "huge.png")
	f, err := os.Create(large)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(common.MaxAttachmentSize+1))
	require.NoError(t, f.Close())
	_, err = prepareAttachment(t.Context(), large)
	require.ErrorContains(t, err, "5 MB")
}

func TestPreparedAttachmentAcceptsUppercaseImageAndOrdinaryText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var pixels bytes.Buffer
	require.NoError(t, png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	path := filepath.Join(dir, "SHOT.PNG")
	require.NoError(t, os.WriteFile(path, pixels.Bytes(), 0o600))
	a, err := prepareAttachment(t.Context(), path)
	require.NoError(t, err)
	require.True(t, a.IsImage())
	require.Equal(t, "image/png", a.MimeType)
	path = filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("merhaba dünya"), 0o600))
	a, err = prepareAttachment(t.Context(), path)
	require.NoError(t, err)
	require.True(t, a.IsText())
	require.Equal(t, "merhaba dünya", string(a.Content))
}
