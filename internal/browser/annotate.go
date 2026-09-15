package browser

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	// MaxScreenshotWidth caps annotated screenshots handed to the
	// model. Boxes are drawn after downscaling so labels stay legible
	// and align 1:1 with the returned image pixels.
	MaxScreenshotWidth = 1600
	// MaxAnnotatedBoxes caps how many element boxes are drawn. A page
	// with thousands of matches gets its first boxes in DOM order;
	// drawing all of them would bury the image in red ink.
	MaxAnnotatedBoxes = 150
	// boxBorder is the box outline thickness in image pixels.
	boxBorder = 3
)

var (
	boxColor   = color.NRGBA{R: 0xFF, A: 0xFF}
	plateColor = color.NRGBA{A: 0xFF}
	labelColor = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
)

// AnnotateScreenshotPNG decodes a page capture, downscales it to at
// most maxWidth, and draws a numbered box over each of elements (CSS
// pixels, scaled by dpr into device pixels first). It returns the
// annotated PNG. Boxes align with the returned image, so the model
// aims by ref label, never by pixel arithmetic. Pure stdlib plus
// x/image, so it runs (and tests) on every platform even though
// captures only originate from a live browser.
func AnnotateScreenshotPNG(data []byte, elements []SnapshotElement, dpr float64, maxWidth int) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding screenshot: %w", err)
	}
	if dpr <= 0 {
		dpr = 1
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("unexpected screenshot size %dx%d", w, h)
	}

	scale := 1.0
	if maxWidth > 0 && w > maxWidth {
		scale = float64(maxWidth) / float64(w)
	}
	dstW := max(1, int(float64(w)*scale))
	dstH := max(1, int(float64(h)*scale))
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)

	// CSS px -> device px -> downscaled image px in one factor.
	px := dpr * scale
	boxes := 0
	for _, el := range elements {
		if boxes >= MaxAnnotatedBoxes {
			break
		}
		r := scaleRect(el.Rect, px)
		if r.Empty() {
			continue
		}
		r = r.Intersect(dst.Bounds())
		if r.Empty() {
			continue
		}
		drawBox(dst, r, el.Ref)
		boxes++
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("encoding annotated screenshot: %w", err)
	}
	return buf.Bytes(), nil
}

