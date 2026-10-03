package experiments

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

type VisualDifference struct {
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	ChangedPixels int64   `json:"changed_pixels"`
	ChangedRatio  float64 `json:"changed_ratio"`
}

// CompareImages reports exact pixel differences without judging aesthetics.
func CompareImages(ctx context.Context, before, after []byte, tolerance uint32) (VisualDifference, error) {
	var images [2]image.Image
	for i, data := range [][]byte{before, after} {
		if len(data) > 16*1024*1024 {
			return VisualDifference{}, fmt.Errorf("image exceeds 16MiB")
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return VisualDifference{}, err
		}
		if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 8_000_000 {
			return VisualDifference{}, fmt.Errorf("image exceeds 8 megapixels")
		}
		images[i], _, err = image.Decode(bytes.NewReader(data))
		if err != nil {
			return VisualDifference{}, err
		}
	}
	a, b := images[0], images[1]
	if a.Bounds().Size() != b.Bounds().Size() {
		return VisualDifference{}, fmt.Errorf("images must have equal dimensions")
	}
	result := VisualDifference{Width: a.Bounds().Dx(), Height: a.Bounds().Dy()}
	for y := 0; y < result.Height; y++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		for x := 0; x < result.Width; x++ {
			ar, ag, ab, aa := a.At(x+a.Bounds().Min.X, y+a.Bounds().Min.Y).RGBA()
			br, bg, bb, ba := b.At(x+b.Bounds().Min.X, y+b.Bounds().Min.Y).RGBA()
			if pixelDelta(ar, br) > tolerance || pixelDelta(ag, bg) > tolerance || pixelDelta(ab, bb) > tolerance || pixelDelta(aa, ba) > tolerance {
				result.ChangedPixels++
			}
		}
	}
	result.ChangedRatio = float64(result.ChangedPixels) / float64(int64(result.Width)*int64(result.Height))
	return result, nil
}

func pixelDelta(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}
