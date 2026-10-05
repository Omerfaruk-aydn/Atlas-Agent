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
)

// BannerPointerSVG is the compact outline used in the control status strip.
//
//go:embed banner-pointer.svg
var BannerPointerSVG string

// Rasterize the same pointer geometry as the SVG at the monitor's DPI.
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

// Paint the rounded, premultiplied badge without covering the main viewport.
func paintBanner(pixels []byte, width, height int, scale float64, elapsed time.Duration) {
	for y := range height {
		for x := range width {
			px, py := (float64(x)+.5)/scale, (float64(y)+.5)/scale
			w, h := float64(width)/scale, float64(height)/scale
			dx, dy := math.Abs(px-w/2)-(w/2-bannerRadius), math.Abs(py-h/2)-(h/2-bannerRadius)
			depth := -(math.Hypot(max(0, dx), max(0, dy)) + min(0, max(dx, dy)) - bannerRadius)
			a := max(0, min(1, depth*scale+.5))
			rim := max(0, min(1, 1.1-depth))
			c := edgeColor((px/w + .15) + float64(elapsed%edgeCycle)/float64(edgeCycle))
			i := (y*width + x) * 4
			for channel, base := range [3]float64{32, 25, 23} {
				pixels[i+channel] = uint8((base*(1-rim) + float64(c[2-channel])*rim) * a)
			}
			pixels[i+3] = uint8(255 * a)
		}
	}
}
