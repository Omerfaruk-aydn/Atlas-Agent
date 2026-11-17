package tools

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func canvasTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{R: 200, G: 90, B: 20, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestDesignCanvasImageSavesWithTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Without a generator the action explains what is missing.
	rsp := runCanvas(t, NewDesignCanvasTool(dir), `{"action":"image","file":"cop","prompt":"a harbour","image":"hero"}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "no image model")

	var gotPrompt, gotAspect string
	gen := func(_ context.Context, prompt, aspect string) ([]byte, string, error) {
		gotPrompt, gotAspect = prompt, aspect
		return canvasTestPNG(t, 64, 32), "image/png", nil
	}
	tool := NewDesignCanvasTool(dir, gen)

	rsp = runCanvas(t, tool, `{"action":"image","file":"harbour","image":"Hero Harbour","prompt":"a ferry at dawn","aspect":"wide"}`)
	require.False(t, rsp.IsError, rsp.Content)
	require.Equal(t, "a ferry at dawn", gotPrompt)
	require.Equal(t, "16:9", gotAspect)
	require.Contains(t, rsp.Content, `image("assets/hero-harbour.png"`)
	require.Contains(t, rsp.Content, "64x32")
	require.NotEmpty(t, rsp.Data, "the picture rides with the text")
	_, err := os.Stat(filepath.Join(dir, ".atlas", "design", "screens", "harbour", "assets", "hero-harbour.png"))
	require.NoError(t, err)

	rsp = runCanvas(t, tool, `{"action":"image","image":"x","prompt":"y"}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "file is required")

	rsp = runCanvas(t, tool, `{"action":"image","file":"cop","image":"x"}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "prompt")

	rsp = runCanvas(t, tool, `{"action":"image","file":"cop","image":"x","prompt":"y","aspect":"cinemascope"}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "aspect")

	failing := NewDesignCanvasTool(dir, func(context.Context, string, string) ([]byte, string, error) {
		return nil, "", errors.New("chatgpt: not signed in")
	})
	rsp = runCanvas(t, failing, `{"action":"image","file":"cop","image":"x","prompt":"y"}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "not signed in")
}

func TestDesignCanvasKnowledgeAndPhotoErrors(t *testing.T) {
	t.Parallel()
	tool := NewDesignCanvasTool(t.TempDir())

	rsp := runCanvas(t, tool, `{"action":"knowledge","topic":"platform","platform":"android"}`)
	require.False(t, rsp.IsError, rsp.Content)
	require.Contains(t, rsp.Content, "Material 3")

	rsp = runCanvas(t, tool, `{"action":"knowledge","topic":"charts","query":"trend over time"}`)
	require.False(t, rsp.IsError, rsp.Content)
	require.Contains(t, rsp.Content, "Line Chart")

	rsp = runCanvas(t, tool, `{"action":"knowledge","topic":"nonsense"}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "topic")

	rsp = runCanvas(t, tool, `{"action":"photo","file":"cop","image":"x"}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "query")
	rsp = runCanvas(t, tool, `{"action":"photo","file":"cop","image":"x","query":"harbour","aspect":"cinema"}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "aspect")

	// The removed screen studio actions are gone.
	for _, a := range []string{"screen_guide", "screen", "screen_image", "screen_patch", "design_system"} {
		rsp = runCanvas(t, tool, `{"action":"`+a+`"}`)
		require.True(t, rsp.IsError, a)
		require.Contains(t, rsp.Content, "unknown action", a)
	}
}
