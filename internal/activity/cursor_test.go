package activity

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCursorHasCrispOutlineAndBoundedHalo(t *testing.T) {
	t.Parallel()
	im := cursorImage()
	require.Equal(t, 112, im.Bounds().Dx())
	require.Equal(t, uint8(0), im.RGBAAt(0, 0).A)
	require.Greater(t, im.RGBAAt(44, 56).A, uint8(0))
	require.Less(t, im.RGBAAt(44, 56).A, uint8(90))
	require.Greater(t, im.RGBAAt(48, 48).R, uint8(170))
	require.Greater(t, im.RGBAAt(56, 73).A, uint8(200), "The new arrow must retain its 26-pixel silhouette")
	for i := 0; i < len(im.Pix); i += 4 {
		require.LessOrEqual(t, im.Pix[i], im.Pix[i+3])
		require.LessOrEqual(t, im.Pix[i+1], im.Pix[i+3])
		require.LessOrEqual(t, im.Pix[i+2], im.Pix[i+3])
	}
	if path := os.Getenv("ATLAS_CURSOR_PREVIEW"); path != "" {
		f, err := os.Create(path)
		require.NoError(t, err)
		defer f.Close()
		require.NoError(t, png.Encode(f, im))
	}
}

func TestCursorStatesKeepHotspotAndPremultipliedPixels(t *testing.T) {
	states := []struct{ pressed, held bool }{{}, {pressed: true}, {held: true}}
	board := image.NewRGBA(image.Rect(0, 0, 672, 224))
	draw.Draw(board, board.Bounds(), image.NewUniform(color.RGBA{23, 25, 32, 255}), image.Point{}, draw.Src)
	for index, state := range states {
		for _, scale := range []float64{1, 1.25, 2, 4} {
			im := cursorStateImage(scale, state.pressed, state.held)
			require.Greater(t, im.RGBAAt(int(48*scale), int(48*scale)).A, uint8(200))
			for i := 0; i < len(im.Pix); i += 4 {
				if im.Pix[i] > im.Pix[i+3] || im.Pix[i+1] > im.Pix[i+3] || im.Pix[i+2] > im.Pix[i+3] {
					t.Fatalf("Non-premultiplied cursor pixel at scale %v, state %d", scale, index)
				}
			}
			if scale == 2 {
				draw.Draw(board, image.Rect(index*224, 0, (index+1)*224, 224), im, image.Point{}, draw.Over)
			}
		}
	}
	if path := os.Getenv("ATLAS_CURSOR_STATES_PREVIEW"); path != "" {
		f, err := os.Create(path)
		require.NoError(t, err)
		defer f.Close()
		require.NoError(t, png.Encode(f, board))
	}
}

func TestCursorPreservesHotspotAtHighDPI(t *testing.T) {
	t.Parallel()
	for _, scale := range []float64{1.25, 1.5, 1.75, 2, 3, 4} {
		im := cursorImageAtScale(scale)
		require.Equal(t, int(112*scale), im.Bounds().Dx())
		require.Zero(t, im.RGBAAt(0, 0).A)
		require.Greater(t, im.RGBAAt(int(48*scale), int(48*scale)).A, uint8(200))
	}
}
