package computer

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScreenshotCropPreservesNativePixels(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	img.Set(5, 6, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	data, err := CropScreenshot(buf.Bytes(), 5, 6, 2, 3)
	require.NoError(t, err)
	crop, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 2, 3), crop.Bounds())
	r, _, _, a := crop.At(0, 0).RGBA()
	require.Equal(t, uint32(65535), r)
	require.Equal(t, uint32(65535), a)
	for _, rect := range [][4]int{{-1, 0, 1, 1}, {9, 9, 2, 2}, {0, 0, 0, 1}, {0, 0, 5000, 1}} {
		_, err := CropScreenshot(buf.Bytes(), rect[0], rect[1], rect[2], rect[3])
		require.Error(t, err)
	}
}
