package browser

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

// solidPNG builds a WxH white PNG for annotation tests.
func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestScaleRectMultipliesByDPR(t *testing.T) {
	t.Parallel()
	got := scaleRect(ElementRect{X: 10, Y: 20, Width: 100, Height: 50}, 2)
	require.Equal(t, image.Rect(20, 40, 220, 140), got)
}

func TestScaleRectRejectsDegenerate(t *testing.T) {
	t.Parallel()
	require.True(t, scaleRect(ElementRect{X: 5, Y: 5}, 2).Empty())
	require.True(t, scaleRect(ElementRect{X: 5, Y: 5, Width: 10, Height: 10}, 0).Empty())
}

func TestElementRectCenter(t *testing.T) {
	t.Parallel()
	x, y := ElementRect{X: 10, Y: 20, Width: 100, Height: 50}.Center()
	require.Equal(t, 60.0, x)
	require.Equal(t, 45.0, y)
}

func TestSnapshotElementCarriesRect(t *testing.T) {
	t.Parallel()
	var el SnapshotElement
	require.NoError(t, json.Unmarshal([]byte(
		`{"ref":"e3","role":"button","tag":"button","name":"Save",`+
			`"rect":{"x":10,"y":20,"width":100,"height":50},"visible":true}`,
	), &el))
	require.Equal(t, "e3", el.Ref)
	require.True(t, el.Visible)
	require.Equal(t, ElementRect{X: 10, Y: 20, Width: 100, Height: 50}, el.Rect)
	x, y := el.Rect.Center()
	require.Equal(t, 60.0, x)
	require.Equal(t, 45.0, y)
}

func TestAnnotateDrawsBoxAndLabel(t *testing.T) {
	t.Parallel()
	els := []SnapshotElement{{
		Ref: "e3", Rect: ElementRect{X: 10, Y: 30, Width: 40, Height: 20},
		Visible: true,
	}}
	data, err := AnnotateScreenshotPNG(solidPNG(t, 200, 100), els, 1, 0)
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 200, 100), img.Bounds())

	isRed := func(c [4]uint32) bool {
		r, g, b, _ := c[0]>>8, c[1]>>8, c[2]>>8, c[3]>>8
		return r > 200 && g < 80 && b < 80
	}
	r, g, b, _ := img.At(10, 30).RGBA()
	require.True(t, isRed([4]uint32{r, g, b, 0}), "box corner should be red")
	r, g, b, _ = img.At(30, 40).RGBA()
	require.False(t, isRed([4]uint32{r, g, b, 0}), "box interior should stay white")

	// The label plate sits above the box: dark pixels must appear there.
	dark := false
	for x := 10; x < 30 && !dark; x++ {
		for y := 13; y < 30 && !dark; y++ {
			rr, gg, bb, _ := img.At(x, y).RGBA()
			if rr>>8 < 80 && gg>>8 < 80 && bb>>8 < 80 {
				dark = true
			}
		}
	}
	require.True(t, dark, "label plate should stamp dark pixels above the box")
}

func TestAnnotateSkipsEmptyRects(t *testing.T) {
	t.Parallel()
	els := []SnapshotElement{{Ref: "e1"}, {Ref: "e9"}}
	data, err := AnnotateScreenshotPNG(solidPNG(t, 100, 60), els, 1, 0)
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 100, 60), img.Bounds())
}

func TestAnnotateDownscalesWideCaptures(t *testing.T) {
	t.Parallel()
	els := []SnapshotElement{{
		Ref: "e1", Rect: ElementRect{X: 0, Y: 0, Width: 3200, Height: 1800},
		Visible: true,
	}}
	data, err := AnnotateScreenshotPNG(solidPNG(t, 3200, 1800), els, 1, MaxScreenshotWidth)
	require.NoError(t, err)
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, MaxScreenshotWidth, cfg.Width)
	require.Equal(t, 900, cfg.Height)
}

func TestAnnotateRejectsGarbage(t *testing.T) {
	t.Parallel()
	_, err := AnnotateScreenshotPNG([]byte{1, 2, 3}, nil, 1, 0)
	require.Error(t, err)
}

func TestAnnotateDefaultsBadDPR(t *testing.T) {
	t.Parallel()
	els := []SnapshotElement{{
		Ref: "e1", Rect: ElementRect{X: 10, Y: 10, Width: 20, Height: 20},
		Visible: true,
	}}
	data, err := AnnotateScreenshotPNG(solidPNG(t, 200, 100), els, 0, 0)
	require.NoError(t, err, "a bad DPR must fall back to 1, not fail")
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 200, 100), img.Bounds())
}
