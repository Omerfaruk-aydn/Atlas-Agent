package tools

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/screens"
	"github.com/stretchr/testify/require"
)

func solid(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestInlineImagesEmbedsProjectFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, solid(4, 4, color.White)))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "img"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "img", "a.png"), buf.Bytes(), 0o644))
	// A photo saved for the design's own screen folder.
	assets := filepath.Join(screens.Dir(dir, "cop"), "assets")
	require.NoError(t, os.MkdirAll(assets, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(assets, "b.png"), buf.Bytes(), 0o644))

	svg := `<image href="img/a.png"/><image href="assets/b.png"/><use href="#x"/><image href="https://example.com/c.png"/><image href="../secret.png"/><image href="img/missing.png"/>`
	out, missing := inlineImages(svg, dir, "cop")
	require.Equal(t, []string{"img/missing.png"}, missing)
	require.Equal(t, 2, strings.Count(out, `href="data:image/png;base64,`))
	require.Contains(t, out, `href="#x"`)
	require.Contains(t, out, `href="https://example.com/c.png"`)
	require.Contains(t, out, `href="../secret.png"`)
}

func TestSheetRowsCutsAnAppSheet(t *testing.T) {
	t.Parallel()
	// Two rows as screens.AppSheet lays them out.
	sheet := solid(900, 2*sheetPad+2*sheetCell+sheetRowGap, color.Gray{200})
	rows := sheetRows(sheet)
	require.Len(t, rows, 2)
	require.Equal(t, sheetCell, rows[0].Bounds().Dy())
	// Any other picture is one row.
	require.Len(t, sheetRows(solid(300, 640, color.Black)), 1)
}

func TestDesignCompareSetsTheRenderBesideReferences(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := design.NewStore(dir)
	doc := design.New("Today")
	doc.Pages[0].Children = []*design.Node{{ID: "s", Type: design.TypeFrame, Name: "01 Today", Width: 393, Height: 852}}
	_, err := store.Save("today", doc, "", true)
	require.NoError(t, err)

	p := DesignCanvasParams{Action: "compare", File: "today"}
	resp := designCompareAction(dir, p)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "render first")

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, solid(393, 852, color.White)))
	exports := filepath.Join(dir, ".atlas", "design", "exports")
	require.NoError(t, os.MkdirAll(exports, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(exports, "today.png"), buf.Bytes(), 0o644))

	resp = designCompareAction(dir, p)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "action references")

	refs := filepath.Join(screens.Dir(dir, "today"), "refs")
	require.NoError(t, os.MkdirAll(refs, 0o755))
	buf.Reset()
	require.NoError(t, jpeg.Encode(&buf, solid(900, 2*sheetPad+3*sheetCell+2*sheetRowGap, color.Gray{120}), nil))
	require.NoError(t, os.WriteFile(filepath.Join(refs, "wallet.jpg"), buf.Bytes(), 0o644))

	resp = designCompareAction(dir, p)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, "image/png", resp.MediaType)
	require.Contains(t, resp.Content, "Reference: <the apps>")
	info, err := os.Stat(filepath.Join(exports, "today-compare.png"))
	require.NoError(t, err)
	require.Positive(t, info.Size())
}
