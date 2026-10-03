package interaction

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCaptureCacheAndSnapshotIsolation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	c := New()
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, img))
	path, err := c.Capture(root, "session", data.Bytes())
	require.NoError(t, err)
	again, err := c.Capture(root, "session", data.Bytes())
	require.NoError(t, err)
	require.Equal(t, path, again)
	stored, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data.Bytes(), stored)
	state := c.Snapshot("session")
	require.NotNil(t, state.Preview)
	state.Preview.Pixels[0] = 0
	require.NotZero(t, c.Snapshot("session").Preview.Pixels[0])
	require.Empty(t, c.ToolSnapshot("session", false).Preview.Pixels)
	require.NotEmpty(t, c.Snapshot("session").Preview.Pixels)
	require.NoError(t, c.Record(root, "session", Entry{Action: "capture", Status: "succeeded"}))
	fresh := New()
	require.NoError(t, fresh.Load(root, "session"))
	require.Equal(t, path, fresh.Snapshot("session").Preview.Path)
	_, err = c.Capture(root, "session", []byte("not an image"))
	require.Error(t, err)
}
