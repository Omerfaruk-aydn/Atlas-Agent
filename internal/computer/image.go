package computer

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
)

// MaxScreenshotWidth caps the width of screenshots handed to the model.
// A full 4K capture costs context on every vision call while rarely
// adding aimable detail, so screenshots are downscaled to this width
// unless the caller explicitly asks for full resolution. Coordinates
// always stay in original screen pixels; DownscaleScreenshot reports
// the scale factor so callers can translate between the two.
const MaxScreenshotWidth = 1600

// ScaledScreenshot is a screenshot downscaled for model consumption,
// plus the data needed to map its pixels back to screen coordinates:
// screenPixel = imagePixel * Scale.
type ScaledScreenshot struct {
	// PNG holds the (possibly downscaled) capture.
	PNG []byte
	// Scale is originalWidth / imageWidth, always >= 1.
	Scale float64
	// ImageSize is the PNG's own dimensions in pixels.
	ImageSize Size
	// ScreenSize is the original capture dimensions in screen pixels.
	ScreenSize Size
}

// DownscaleScreenshot decodes a PNG capture and, when it is wider than
// maxWidth, shrinks it with a box average filter. Images at or below
// maxWidth pass through untouched with Scale 1: no quality is lost
// where none needs saving. Pure stdlib, so it runs (and tests) on
// every platform even though captures only originate on Windows.
func DownscaleScreenshot(data []byte, maxWidth int) (ScaledScreenshot, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return ScaledScreenshot{}, fmt.Errorf("computer-use: decode screenshot: %w", err)
	}
	bounds := img.Bounds()
	screen := Size{Width: bounds.Dx(), Height: bounds.Dy()}
	if screen.Width <= 0 || screen.Height <= 0 {
		return ScaledScreenshot{}, fmt.Errorf(
			"computer-use: unexpected screenshot size %dx%d",
			screen.Width, screen.Height,
		)
	}
	if maxWidth <= 0 || screen.Width <= maxWidth {
		return ScaledScreenshot{
			PNG:        data,
			Scale:      1,
			ImageSize:  screen,
			ScreenSize: screen,
		}, nil
	}

	scaled := boxDownscale(img, maxWidth)
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return ScaledScreenshot{}, fmt.Errorf("computer-use: encode scaled screenshot: %w", err)
	}
	return ScaledScreenshot{
		PNG:        buf.Bytes(),
		Scale:      float64(screen.Width) / float64(scaled.Bounds().Dx()),
		ImageSize:  Size{Width: scaled.Bounds().Dx(), Height: scaled.Bounds().Dy()},
		ScreenSize: screen,
	}, nil
}

// boxDownscale shrinks img to dstWidth, preserving aspect ratio, by
// averaging each destination pixel's source box. Averaging (rather
// than point sampling) keeps thin details like text strokes legible
// instead of dropping them between samples.
func boxDownscale(img image.Image, dstWidth int) *image.NRGBA {
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	dstH := max(1, srcH*dstWidth/srcW)
	dst := image.NewNRGBA(image.Rect(0, 0, dstWidth, dstH))

	xRatio := float64(srcW) / float64(dstWidth)
	yRatio := float64(srcH) / float64(dstH)
	for dy := 0; dy < dstH; dy++ {
		y0 := int(float64(dy) * yRatio)
		y1 := max(y0+1, int(float64(dy+1)*yRatio))
		for dx := 0; dx < dstWidth; dx++ {
			x0 := int(float64(dx) * xRatio)
			x1 := max(x0+1, int(float64(dx+1)*xRatio))
			var r, g, b, a uint32
			var n uint32
			for sy := y0; sy < y1 && sy < bounds.Max.Y; sy++ {
				for sx := x0; sx < x1 && sx < bounds.Max.X; sx++ {
					cr, cg, cb, ca := img.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					r += cr >> 8
					g += cg >> 8
					b += cb >> 8
					a += ca >> 8
					n++
				}
			}
			if n == 0 {
				continue
			}
			i := dst.PixOffset(dx, dy)
			dst.Pix[i] = uint8(r / n)
			dst.Pix[i+1] = uint8(g / n)
			dst.Pix[i+2] = uint8(b / n)
			dst.Pix[i+3] = uint8(a / n)
		}
	}
	return dst
}
