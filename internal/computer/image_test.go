package computer

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// encodeSolid builds a WxH PNG with distinct quadrant colors so a
// broken resampler cannot hide behind uniform pixels.
func encodeSolid(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i] = uint8(x * 255 / max(w-1, 1))
			img.Pix[i+1] = uint8(y * 255 / max(h-1, 1))
			img.Pix[i+2] = 0x80
			img.Pix[i+3] = 0xFF
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode = %v", err)
	}
	return buf.Bytes()
}

func TestDownscalePassesThroughSmallImages(t *testing.T) {
	t.Parallel()
	raw := encodeSolid(t, 800, 600)
	got, err := DownscaleScreenshot(raw, MaxScreenshotWidth)
	if err != nil {
		t.Fatalf("DownscaleScreenshot = %v, want nil", err)
	}
	if !bytes.Equal(got.PNG, raw) {
		t.Fatal("small capture was re-encoded instead of passing through")
	}
	if got.Scale != 1 {
		t.Fatalf("Scale = %v, want 1", got.Scale)
	}
}

func TestDownscaleShrinksWideImages(t *testing.T) {
	t.Parallel()
	got, err := DownscaleScreenshot(encodeSolid(t, 3200, 1800), MaxScreenshotWidth)
	if err != nil {
		t.Fatalf("DownscaleScreenshot = %v, want nil", err)
	}
	if got.ImageSize != (Size{Width: 1600, Height: 900}) {
		t.Fatalf("ImageSize = %+v, want 1600x900", got.ImageSize)
	}
	if got.ScreenSize != (Size{Width: 3200, Height: 1800}) {
		t.Fatalf("ScreenSize = %+v, want 3200x1800", got.ScreenSize)
	}
	if got.Scale != 2 {
		t.Fatalf("Scale = %v, want 2", got.Scale)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(got.PNG))
	if err != nil {
		t.Fatalf("scaled PNG does not decode: %v", err)
	}
	if cfg.Width != 1600 || cfg.Height != 900 {
		t.Fatalf("decoded size = %dx%d, want 1600x900", cfg.Width, cfg.Height)
	}
}

func TestDownscalePreservesGradientDirection(t *testing.T) {
	t.Parallel()
	got, err := DownscaleScreenshot(encodeSolid(t, 3200, 100), 1600)
	if err != nil {
		t.Fatalf("DownscaleScreenshot = %v, want nil", err)
	}
	img, err := png.Decode(bytes.NewReader(got.PNG))
	if err != nil {
		t.Fatalf("png.Decode = %v", err)
	}
	// The source ramps red left-to-right; the scaled image must keep
	// that order instead of smearing it.
	left := img.At(0, 0)
	right := img.At(1599, 0)
	lr, _, _, _ := left.RGBA()
	rr, _, _, _ := right.RGBA()
	if lr>>8 >= rr>>8 {
		t.Fatalf("gradient reversed after downscale: left=%d right=%d", lr>>8, rr>>8)
	}
}

func TestDownscaleRejectsGarbage(t *testing.T) {
	t.Parallel()
	if _, err := DownscaleScreenshot([]byte{1, 2, 3}, MaxScreenshotWidth); err == nil {
		t.Fatal("DownscaleScreenshot(garbage) = nil, want error")
	}
}
