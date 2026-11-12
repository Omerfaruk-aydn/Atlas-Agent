package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderStampRoundTrips(t *testing.T) {
	t.Parallel()
	doc := New("stamp")
	page := doc.Pages[0].ID
	require.False(t, doc.Rendered(page))
	doc.NoteRender(page)
	require.True(t, doc.Rendered(page))
	data, err := Marshal(doc)
	require.NoError(t, err)
	again, err := Parse(data)
	require.NoError(t, err)
	require.True(t, again.Rendered(again.Pages[0].ID))
	again.Pages[0].Background = "#111111"
	require.False(t, again.Rendered(again.Pages[0].ID))
	report := LintReport{Score: 100}
	checked := report.WithRenderCheck(again, again.Pages[0].ID)
	require.Equal(t, 92, checked.Score)
	require.Equal(t, "render", checked.Issues[0].Rule)
	require.Equal(t, 100, report.WithRenderCheck(doc, page).Score)
}

func TestCritiqueNamesTheRubric(t *testing.T) {
	t.Parallel()
	doc, _, err := Apply(New("critique"), "", []Op{
		{Op: "set_theme", Name: "zinc"},
		{Op: "insert", Block: "web/hero", ID: "hero"},
	})
	require.NoError(t, err)
	text, err := Critique(doc, "", "exports/critique.png")
	require.NoError(t, err)
	for _, line := range []string{"hierarchy", "spacing", "alignment", "contrast", "copy", "brand", "emptiness", "exports/critique.png"} {
		require.Contains(t, text, line)
	}
}

func TestRasterWritesAPNG(t *testing.T) {
	pw := PlaywrightDir(".")
	if pw == "" {
		t.Skip("playwright-core is not installed")
	}
	if _, err := os.Stat(filepath.Join(pw, "package.json")); err != nil {
		t.Skip("playwright-core is not installed")
	}
	doc, _, err := Apply(New("raster"), "", []Op{
		{Op: "set_theme", Name: "zinc"},
		{Op: "insert", Block: "web/button", ID: "button"},
	})
	require.NoError(t, err)
	dir := t.TempDir()
	svgPath := filepath.Join(dir, "button.svg")
	pngPath := filepath.Join(dir, "button.png")
	svg, err := SVG(doc, doc.Pages[0], nil)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(svgPath, []byte(svg), 0o644))
	require.NoError(t, Raster(svgPath, pngPath, FontsDir(pw), pw))
	png, err := os.ReadFile(pngPath)
	require.NoError(t, err)
	require.Greater(t, len(png), 500)
	require.Equal(t, []byte{0x89, 'P', 'N', 'G'}, png[:4])
}
