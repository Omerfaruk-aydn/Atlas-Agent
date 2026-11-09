package tools

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func solidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func savePNGImage(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

func TestCompareImagesIsZeroForIdenticalAndLocatesADifference(t *testing.T) {
	t.Parallel()
	white := color.RGBA{255, 255, 255, 255}
	same := CompareImages(solidImage(60, 60, white), solidImage(60, 60, white), 16)
	require.Zero(t, same.ChangedRatio)

	edited := solidImage(60, 60, white)
	for y := range 20 {
		for x := range 20 {
			edited.SetRGBA(x, y, color.RGBA{0, 0, 0, 255}) // The top-left ninth.
		}
	}
	diff := CompareImages(solidImage(60, 60, white), edited, 16)
	require.InDelta(t, 1.0/9, diff.ChangedRatio, 0.001)
	require.InDelta(t, 1.0, diff.Grid[0][0], 0.001, "the change is in the top-left region")
	require.Zero(t, diff.Grid[2][2])
}

func TestCompareImagesToleratesSmallColourShiftsAndScalesTheCandidate(t *testing.T) {
	t.Parallel()
	a := solidImage(100, 80, color.RGBA{100, 100, 100, 255})
	b := solidImage(50, 40, color.RGBA{108, 108, 108, 255}) // Half the size, 8 levels off.
	require.Zero(t, CompareImages(a, b, 16).ChangedRatio)
	require.InDelta(t, 1.0, CompareImages(a, b, 4).ChangedRatio, 0.001)
}

func TestImageCompareToolWritesADiffAndRejectsBadInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	savePNGImage(t, filepath.Join(dir, "ref.png"), solidImage(40, 40, color.RGBA{255, 255, 255, 255}))
	savePNGImage(t, filepath.Join(dir, "cand.png"), solidImage(40, 40, color.RGBA{0, 0, 0, 255}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644))
	tool := NewImageCompareTool(dir)

	run := func(input string) fantasy.ToolResponse {
		resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "1", Name: ImageCompareToolName, Input: input})
		require.NoError(t, err)
		return resp
	}

	ok := run(`{"reference_path":"ref.png","candidate_path":"cand.png"}`)
	require.False(t, ok.IsError)
	require.Contains(t, ok.Content, "100.0% of pixels differ")
	require.Contains(t, ok.Content, "clearly different")
	matches, err := filepath.Glob(filepath.Join(dir, ".atlas", "compare", "diff-*.png"))
	require.NoError(t, err)
	require.Len(t, matches, 1)

	require.True(t, run(`{"reference_path":"ref.png","candidate_path":"missing.png"}`).IsError)
	require.True(t, run(`{"reference_path":"ref.png","candidate_path":"note.txt"}`).IsError)
	require.True(t, run(`{"reference_path":"ref.png"}`).IsError)
	require.True(t, run(`{"reference_path":"ref.png","candidate_path":"cand.png","tolerance":999}`).IsError)
}
