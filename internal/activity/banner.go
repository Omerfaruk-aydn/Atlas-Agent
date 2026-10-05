package activity

import (
	"context"
	_ "embed"
	"math"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

type languageKey struct{}

const (
	bannerHeight = 38
	bannerRadius = 6
	// The character slot starts here; the caption follows it.
	bannerMascotLeft = 6
	bannerTextLeft   = 46
)

// BannerMascotSVG is the static character shown before the first animated
// frame and whenever animation is unavailable.
//
//go:embed banner-mascot.svg
var BannerMascotSVG string

// The original static pointer icon, kept as the native render fallback.
func bannerPointerCoverage(x, y, scale float64) float64 {
	if x < 16 || x > 34 || y < 9 || y > 29 {
		return 0
	}
	points := [][2]float64{{2, 2}, {17, 13}, {10, 13}, {7, 20}}
	_, distance := cursorPolygon((x-16)/.9, (y-9)/.9, points)
	return max(0, min(1, (.65-distance)*.9*scale+.5))
}

// WithLanguage carries the interface locale into desktop and browser overlays.
func WithLanguage(ctx context.Context, language string) context.Context {
	return context.WithValue(ctx, languageKey{}, language)
}

// BannerText returns translated, Atlas-authored control status and stop labels.
func BannerText(e Event) (string, string) {
	caption := "Atlas is using your computer"
	if e.Resource == "browser" {
		caption = "Atlas is using your browser"
	}
	return i18n.Text(e.Language, caption), i18n.Text(e.Language, "Stop")
}

// bannerRimPixel is one antialiased edge pixel whose color cycles; every
// other banner pixel is static between layout changes.
type bannerRimPixel struct {
	index           int
	hue, rim, alpha float64
}

// Paint the rounded, premultiplied badge without covering the main viewport
// and return the edge pixels that animate.
func paintBanner(pixels []byte, width, height int, scale float64, elapsed time.Duration) []bannerRimPixel {
	var edge []bannerRimPixel
	for y := range height {
		for x := range width {
			px, py := (float64(x)+.5)/scale, (float64(y)+.5)/scale
			w, h := float64(width)/scale, float64(height)/scale
			dx, dy := math.Abs(px-w/2)-(w/2-bannerRadius), math.Abs(py-h/2)-(h/2-bannerRadius)
			depth := -(math.Hypot(max(0, dx), max(0, dy)) + min(0, max(dx, dy)) - bannerRadius)
			a := max(0, min(1, depth*scale+.5))
			rim := max(0, min(1, 1.1-depth))
			i := (y*width + x) * 4
			if rim > 0 && a > 0 {
				edge = append(edge, bannerRimPixel{index: i, hue: px/w + .15, rim: rim, alpha: a})
			}
			c := edgeColor((px/w + .15) + float64(elapsed%edgeCycle)/float64(edgeCycle))
			for channel, base := range [3]float64{32, 25, 23} {
				pixels[i+channel] = uint8((base*(1-rim) + float64(c[2-channel])*rim) * a)
			}
			pixels[i+3] = uint8(255 * a)
		}
	}
	return edge
}

// paintBannerRim recolors only the cycling edge, matching paintBanner.
func paintBannerRim(pixels []byte, edge []bannerRimPixel, elapsed time.Duration) {
	phase := float64(elapsed%edgeCycle) / float64(edgeCycle)
	for _, p := range edge {
		c := edgeColor(p.hue + phase)
		for channel, base := range [3]float64{32, 25, 23} {
			pixels[p.index+channel] = uint8((base*(1-p.rim) + float64(c[2-channel])*p.rim) * p.alpha)
		}
	}
}
