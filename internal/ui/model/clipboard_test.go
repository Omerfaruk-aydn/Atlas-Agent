package model

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

func TestClipboardPreparesMultipleImagesTextAndVideo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.White)
	var pixels bytes.Buffer
	require.NoError(t, png.Encode(&pixels, img))
	paths := []string{filepath.Join(dir, "resim.png"), filepath.Join(dir, "notlar.json"), filepath.Join(dir, "clip.mp4")}
	require.NoError(t, os.WriteFile(paths[0], pixels.Bytes(), 0o600))
	require.NoError(t, os.WriteFile(paths[1], []byte(`{"task":"merhaba"}`), 0o600))
	require.NoError(t, os.WriteFile(paths[2], []byte("video placeholder"), 0o600))
	result := prepareFiles(t.Context(), 4, paths)
	require.NoError(t, result.err)
	require.Len(t, result.attachments, 3)
	require.True(t, result.attachments[0].IsImage())
	require.True(t, result.attachments[1].IsText(), "JSON must be usable text rather than unsupported binary")
	require.True(t, result.attachments[2].IsText(), "Video must be a local reference, not an arbitrary binary upload")
	require.Contains(t, string(result.attachments[2].Content), "video tool")
	require.NotContains(t, string(result.attachments[2].Content), "video placeholder")
	result = prepareFiles(t.Context(), 4, append(paths, paths[0]))
	require.Len(t, result.attachments, 3, "A duplicated file-manager entry must not attach twice")
}

func TestClipboardRetainsValidFilesWhenOneFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "valid.txt")
	require.NoError(t, os.WriteFile(path, []byte("good"), 0o600))
	result := prepareFiles(t.Context(), 1, []string{dir, path})
	require.Error(t, result.err)
	require.Len(t, result.attachments, 1)
	require.Equal(t, "good", string(result.attachments[0].Content))
	_, err := prepareAttachment(t.Context(), dir)
	require.Error(t, err)
}

func TestClipboardTextAndExistingPaths(t *testing.T) {
	t.Parallel()
	result := preparePaste(t.Context(), 9, "hello\r\nworld")
	require.Equal(t, "hello\nworld", result.text)
	path := filepath.Join(t.TempDir(), "özel dosya.txt")
	require.NoError(t, os.WriteFile(path, []byte("contents"), 0o600))
	result = preparePaste(t.Context(), 9, path)
	require.Len(t, result.attachments, 1)
	require.Empty(t, result.text)
	result = preparePaste(t.Context(), 9, path+" missing")
	require.Empty(t, result.attachments)
	require.Equal(t, path+" missing", result.text)
}

func TestClipboardStaleResultIsIgnored(t *testing.T) {
	t.Parallel()
	m := &UI{draftGen: 7}
	require.Nil(t, m.handleClipboard(clipboardMsg{gen: 6, attachments: []message.Attachment{{Content: []byte("old")}}}))
}
