package activity

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRGBEdge4KPreview(t *testing.T) {
	path := os.Getenv("ATLAS_EDGE_PREVIEW")
	if path == "" {
		t.Skip("Set ATLAS_EDGE_PREVIEW to export the 4K native renderer artwork")
	}
	const width, height = 3840, 2160
	im := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.RGBA{19, 20, 27, 255}), image.Point{}, draw.Src)
	for side, b := range edgeBounds(0, 0, width, height, 2) {
		s := newEdgePixels(width, height, 2, side)
		pixels := make([]byte, s.width*s.height*4)
		s.paint(pixels, 0)
		layer := image.NewRGBA(image.Rect(0, 0, s.width, s.height))
		for i := 0; i < len(pixels); i += 4 {
			layer.Pix[i], layer.Pix[i+1], layer.Pix[i+2], layer.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], pixels[i+3]
		}
		draw.Draw(im, image.Rect(b.X, b.Y, b.X+b.W, b.Y+b.H), layer, image.Point{}, draw.Over)
	}
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, png.Encode(f, im))
}

func TestEdgeStripsCoverPerimeterWithoutCoveringCenter(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 2, 4} {
		bounds := edgeBounds(-3840, -2160, 3840, 2160, scale)
		require.Len(t, bounds, 4)
		area := 0
		for _, b := range bounds {
			require.Positive(t, b.W)
			require.Positive(t, b.H)
			area += b.W * b.H
			require.False(t, b.X <= -1920 && b.X+b.W > -1920 && b.Y <= -1080 && b.Y+b.H > -1080)
		}
		d := bounds[0].H
		require.Equal(t, 3840*2160-(3840-2*d)*(2160-2*d), area)
	}
}

func TestEdgePixelsAreFeatheredPremultipliedAndAnimate(t *testing.T) {
	s := newEdgePixels(640, 360, 1, 0)
	pixels := make([]byte, s.width*s.height*4)
	s.paint(pixels, 0)
	initial := append([]byte(nil), pixels...)
	for i := 0; i < len(pixels); i += 4 {
		require.LessOrEqual(t, pixels[i], pixels[i+3])
		require.LessOrEqual(t, pixels[i+1], pixels[i+3])
		require.LessOrEqual(t, pixels[i+2], pixels[i+3])
	}
	center := s.width / 2
	require.Greater(t, pixels[center*4+3], pixels[((s.height-2)*s.width+center)*4+3])
	require.Zero(t, pixels[((s.height-1)*s.width+center)*4+3])
	s.paint(pixels, edgeCycle/3)
	require.NotEqual(t, initial, pixels)
	s.paint(pixels, edgeCycle)
	require.Equal(t, initial, pixels)
	require.Zero(t, testing.AllocsPerRun(10, func() { s.paint(pixels, time.Second) }))
}

func TestMainFrameHasRoundedCornersWithoutSeparateAccents(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 2, 4} {
		s := newEdgePixels(320, 180, scale, 0)
		require.Zero(t, s.alpha[0], "The square corner lies outside the rounded frame")
		require.Zero(t, s.alpha[319])
		x := int(math.Round(6 * scale))
		y := int(math.Round(22 * scale))
		require.LessOrEqual(t, s.alpha[y*s.width+x], max(s.alpha[y*s.width+x-1], s.alpha[y*s.width+x+1]), "There is no isolated inset bracket peak")
		curve := int(math.Round(6 * scale))
		require.Greater(t, s.alpha[curve*s.width+curve], byte(25), "The main rim follows the rounded boundary")
	}
}

func TestSmokeRemainsVisibleBeyondTheRimAndDrifts(t *testing.T) {
	s := newEdgePixels(1920, 1080, 1, 0)
	pixels := make([]byte, s.width*s.height*4)
	s.paint(pixels, 0)
	at := (30*s.width + s.width/2) * 4
	require.Greater(t, pixels[at+3], byte(50), "Smoke must remain clearly visible 30px inside the frame")
	initial := pixels[at+3]
	s.paint(pixels, edgeCycle/4)
	require.NotEqual(t, initial, pixels[at+3], "Smoke changes shape/density, not only color")
	require.LessOrEqual(t, edgeCycle, 8*time.Second)
}

func TestSmokeIsContinuousAcrossNativeSurfaceJoins(t *testing.T) {
	top := newEdgePixels(1920, 1080, 1, 0)
	left := newEdgePixels(1920, 1080, 1, 3)
	topPixels := make([]byte, top.width*top.height*4)
	leftPixels := make([]byte, left.width*left.height*4)
	for _, elapsed := range []time.Duration{0, edgeCycle / 4, edgeCycle / 2} {
		top.paint(topPixels, elapsed)
		left.paint(leftPixels, elapsed)
		for channel := range 4 {
			a := topPixels[((top.height-1)*top.width+20)*4+channel]
			b := leftPixels[20*4+channel]
			require.LessOrEqual(t, absEdge(int(a)-int(b)), 3, "Adjacent surfaces must sample the same smoke field")
		}
	}
}

func TestCornerSmokeHasNoDiagonalCrease(t *testing.T) {
	s := newEdgePixels(1920, 1080, 1, 0)
	pixels := make([]byte, s.width*s.height*4)
	for _, elapsed := range []time.Duration{0, edgeCycle / 4, edgeCycle / 2} {
		s.paint(pixels, elapsed)
		for _, depth := range []int{30, 40, 50, 60} {
			center := int(pixels[(depth*s.width+depth)*4+3])
			left := int(pixels[((depth+2)*s.width+depth-2)*4+3])
			right := int(pixels[((depth-2)*s.width+depth+2)*4+3])
			// Allow four alpha levels for LUT and 8-bit opacity quantization.
			require.LessOrEqual(t, absEdge(left+right-2*center), 4, "The corner fog must curve smoothly across the diagonal at depth %d and time %s", depth, elapsed)
		}
	}
}

func BenchmarkEdgeFrame4K(b *testing.B) {
	var surfaces [4]*edgePixels
	var pixels [4][]byte
	for i := range surfaces {
		surfaces[i] = newEdgePixels(3840, 2160, 1.5, i)
		pixels[i] = make([]byte, surfaces[i].width*surfaces[i].height*4)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for i, s := range surfaces {
			s.paint(pixels[i], time.Duration(n)*time.Second/60)
		}
	}
}
