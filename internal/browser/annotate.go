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

// scaleRect maps a CSS-pixel box into image pixels, rounding outward
// so thin elements keep at least one pixel of box.
func scaleRect(r ElementRect, s float64) image.Rectangle {
	if r.Width <= 0 || r.Height <= 0 || s <= 0 {
		return image.Rectangle{}
	}
	x0 := int(r.X * s)
	y0 := int(r.Y * s)
	x1 := int(r.X*s + r.Width*s + 0.999)
	y1 := int(r.Y*s + r.Height*s + 0.999)
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	return image.Rect(x0, y0, x1, y1)
}

// drawBox outlines r and stamps its ref label above the top-left
// corner (or inside the box when there is no room above).
func drawBox(dst *image.NRGBA, r image.Rectangle, ref string) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			edge := x-r.Min.X < boxBorder || r.Max.X-1-x < boxBorder ||
				y-r.Min.Y < boxBorder || r.Max.Y-1-y < boxBorder
			if edge {
				dst.Set(x, y, boxColor)
			}
		}
	}
	drawLabel(dst, r, ref)
}

// drawLabel stamps ref on a black plate so basicfont's small white
// glyphs stay readable over any page background.
func drawLabel(dst *image.NRGBA, r image.Rectangle, ref string) {
	const charW, charH = 7, 13
	pw := len(ref)*charW + 4
	ph := charH + 4
	px, py := r.Min.X, r.Min.Y-ph
	if py < dst.Bounds().Min.Y {
		py = r.Min.Y
	}
	plate := image.Rect(px, py, px+pw, py+ph).Intersect(dst.Bounds())
	if plate.Empty() {
		return
	}
	draw.Draw(dst, plate, &image.Uniform{plateColor}, image.Point{}, draw.Src)
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(labelColor),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(plate.Min.X+2, plate.Min.Y+ph-3),
	}
	d.DrawString(ref)
}
