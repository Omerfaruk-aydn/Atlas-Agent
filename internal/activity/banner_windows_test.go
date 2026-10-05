//go:build windows

package activity

import (
	"image"
	"image/png"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeBannerRendersReadableTextAtHighDPI(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 2, 4} {
		var b nativeBanner
		require.True(t, b.prepare(overlayRect{0, 0, 3840, 2160}, scale, Event{Language: "tr", CanStop: true}))
		require.Equal(t, int(math.Round(38*scale)), b.surface.model.height, "The slim strip must retain its height at fractional and high DPI")
		b.paint(0)
		bright := 0
		for _, a := range b.glyphs {
			if a > 128 {
				bright++
			}
		}
		require.Greater(t, bright, 300, "The native status must contain actual antialiased text glyphs")
		pixels := b.surface.pixels
		at := func(x, y float64) byte {
			i := (int(y*scale)*b.surface.model.width + int(x*scale)) * 4
			return max(pixels[i], pixels[i+1], pixels[i+2])
		}
		require.Greater(t, at(25, 16), byte(120), "The status icon needs a crisp pointer outline")
		require.Less(t, at(23, 21), byte(80), "The pointer interior must stay hollow")
		require.Zero(t, pixels[3], "Rounded corners must remain transparent")
		for i := 0; i < len(pixels); i += 4 {
			require.LessOrEqual(t, pixels[i], pixels[i+3])
			require.LessOrEqual(t, pixels[i+1], pixels[i+3])
			require.LessOrEqual(t, pixels[i+2], pixels[i+3])
		}
		if path := os.Getenv("ATLAS_BANNER_PREVIEW"); path != "" && scale == 2 {
			im := image.NewRGBA(image.Rect(0, 0, b.surface.model.width, b.surface.model.height))
			for i := 0; i < len(pixels); i += 4 {
				im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], pixels[i+3]
			}
			f, err := os.Create(path)
			require.NoError(t, err)
			require.NoError(t, png.Encode(f, im))
			require.NoError(t, f.Close())
		}
		b.close()
	}
}

func TestDesktopEscapeIgnoresInjectedKeys(t *testing.T) {
	require.True(t, physicalEscape(0, 0x100, keyboardData{Key: 0x1b}))
	for _, key := range []keyboardData{{Key: 0x1b, Flags: 0x10}, {Key: 0x1b, Flags: 2}, {Key: 13}} {
		require.False(t, physicalEscape(0, 0x100, key))
	}
	require.False(t, physicalEscape(-1, 0x100, keyboardData{Key: 0x1b}))
	require.False(t, physicalEscape(0, 0x101, keyboardData{Key: 0x1b}))
}

func TestNativeBannerKeepsStopAreaOnNarrowMonitor(t *testing.T) {
	for _, language := range []string{"en", "tr", "de", "fr", "it", "ar"} {
		var b nativeBanner
		require.True(t, b.prepare(overlayRect{0, 0, 640, 480}, 2, Event{Language: language, CanStop: true}))
		require.LessOrEqual(t, b.surface.model.width, 608)
		// The caption may ellipsize, but it must not consume the stop area.
		bright := 0
		w := b.surface.model.width
		for y := range b.surface.model.height {
			for x := w - int(b.tail); x < w-32; x++ {
				if b.glyphs[y*w+x] > 128 {
					bright++
				}
			}
		}
		require.Greater(t, bright, 100, "Esc and the translated stop label must stay readable: %s", language)
		b.close()
	}
}
