package experiments

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareImagesMeasuresPixelsAndRejectsDimensions(t *testing.T) {
	t.Parallel()
	before := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	after := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	after.Set(1, 1, color.NRGBA{R: 255, A: 255})
	encode := func(img image.Image) []byte {
		var buffer bytes.Buffer
		require.NoError(t, png.Encode(&buffer, img))
		return buffer.Bytes()
	}
	result, err := CompareImages(t.Context(), encode(before), encode(after), 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.ChangedPixels)
	require.Equal(t, 0.25, result.ChangedRatio)
	_, err = CompareImages(t.Context(), encode(before), encode(image.NewNRGBA(image.Rect(0, 0, 1, 1))), 0)
	require.ErrorContains(t, err, "equal dimensions")
}
