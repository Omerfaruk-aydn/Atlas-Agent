package tools

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPreviewTransparencyIsNotPremultipliedTwice(t *testing.T) {
	t.Parallel()
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			src.SetNRGBA(x, y, color.NRGBA{R: 255, A: 128})
		}
	}
	scaled := boxScale(src, 1, 1)
	dst := image.NewRGBA(image.Rect(0, 0, 1, 1))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), scaled, image.Point{}, draw.Over)
	require.Equal(t, color.RGBA{R: 255, G: 127, B: 127, A: 255}, dst.RGBAAt(0, 0))
}

// noisePNG encodes a w×h image of random pixels, which PNG cannot
// compress, so its size grows with its area.
func noisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.IntN(256))
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// paddedPNG returns a valid PNG of exactly size bytes: a w×h image with an
// ancillary chunk inserted before IEND. Decoders skip ancillary chunks, so
// the file is a real image of a precise length.
func paddedPNG(t *testing.T, w, h, size int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	base := buf.Bytes()
	iend := base[len(base)-12:]
	head := base[:len(base)-12]
	pad := size - len(base) - 12
	require.GreaterOrEqual(t, pad, 0, "base PNG already larger than target")
	chunk := make([]byte, 0, pad+12)
	chunk = binary.BigEndian.AppendUint32(chunk, uint32(pad))
	body := append([]byte("atLs"), make([]byte, pad)...)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
	out := append(append(append([]byte{}, head...), chunk...), iend...)
	require.Len(t, out, size)
	_, err := png.Decode(bytes.NewReader(out))
	require.NoError(t, err)
	return out
}

func TestImageDeliveryLimitIsSeparateFromText(t *testing.T) {
	t.Parallel()
	require.Equal(t, 2_097_152, MaxImageDeliveryBytes)
	require.Equal(t, 200*1024, MaxViewSize, "the text limit must not grow with the image limit")
	require.Equal(t, 2_796_204, ImageBase64Len(MaxImageDeliveryBytes))
	require.Equal(t, 0, ImageBase64Len(0))
	require.Equal(t, 4, ImageBase64Len(1))
	require.Equal(t, 4, ImageBase64Len(3))
	require.Equal(t, 8, ImageBase64Len(4))
}

func TestPrepareImageAcceptsTheExactLimitUnchanged(t *testing.T) {
	t.Parallel()
	data := paddedPNG(t, 64, 48, MaxImageDeliveryBytes)
	d, err := PrepareImageForDelivery(data, "image/png")
	require.NoError(t, err)
	require.False(t, d.Preview)
	require.Equal(t, data, d.Data, "an image at the limit is delivered byte for byte")
	require.Equal(t, MaxImageDeliveryBytes, d.RawBytes)
	require.Equal(t, 2_796_204, d.Base64Bytes)
	require.Equal(t, "image/png", d.MediaType)
	require.Equal(t, 64, d.Width)
	require.Equal(t, "model", d.Recipient)
}

func TestPrepareImageOneByteOverTheLimitDeliversAMarkedPreview(t *testing.T) {
	t.Parallel()
	data := paddedPNG(t, 1920, 1080, MaxImageDeliveryBytes+1)
	original := bytes.Clone(data)
	d, err := PrepareImageForDelivery(data, "image/png")
	require.NoError(t, err)
	require.True(t, d.Preview)
	require.Equal(t, original, data, "the original bytes are never modified")
	require.LessOrEqual(t, d.RawBytes, MaxImageDeliveryBytes)
	require.Equal(t, len(d.Data), d.RawBytes)
	require.Equal(t, "image/jpeg", d.MediaType)
	require.Equal(t, MaxImageDeliveryBytes+1, d.OriginalBytes)
	require.Equal(t, 1920, d.OriginalWidth)
	require.Equal(t, "image/png", d.OriginalType)
	require.LessOrEqual(t, d.Width, 1600)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(d.Data))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, d.Width, cfg.Width)
	require.Equal(t, d.Height, cfg.Height)
	require.Contains(t, d.Note(), "Preview")
	require.Contains(t, d.Note(), "original file is unchanged")
	require.InDelta(t, 1920.0/float64(d.Width), d.CoordinateScale, 1e-9)
}

// gradientPNG is a w×h image that compresses well, so a very tall one is
// still small in bytes, like a full-page screenshot of a flat page.
func gradientPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 8), G: uint8(y / 16), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestPrepareImageScalesATallScreenshotUnderTheSideLimit(t *testing.T) {
	t.Parallel()
	data := gradientPNG(t, 1280, 9000)
	require.Less(t, len(data), MaxImageDeliveryBytes, "small in bytes, tall in pixels")
	d, err := PrepareImageForDelivery(data, "image/png")
	require.NoError(t, err)
	require.True(t, d.Preview)
	require.Equal(t, "image/jpeg", d.MediaType)
	require.LessOrEqual(t, d.Height, MaxImageDeliverySide)
	require.LessOrEqual(t, d.Width, 1280)
	require.Equal(t, 9000, d.OriginalHeight)
	// The aspect ratio is kept, within one pixel of rounding.
	require.InDelta(t, 1280.0/9000.0, float64(d.Width)/float64(d.Height), 0.01)
	require.InDelta(t, 9000.0/float64(d.Height), d.CoordinateScaleY, 1e-9)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(d.Data))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, d.Height, cfg.Height)
	require.Contains(t, d.Note(), "per side")
	require.NotContains(t, d.Note(), "byte image limit")
}

func TestPrepareImageKeepsTheSideLimitExactAndScalesOnePixelOver(t *testing.T) {
	t.Parallel()
	atLimit := gradientPNG(t, 40, MaxImageDeliverySide)
	d, err := PrepareImageForDelivery(atLimit, "image/png")
	require.NoError(t, err)
	require.False(t, d.Preview)
	require.Equal(t, atLimit, d.Data)

	over := gradientPNG(t, 40, MaxImageDeliverySide+1)
	d, err = PrepareImageForDelivery(over, "image/png")
	require.NoError(t, err)
	require.True(t, d.Preview)
	require.LessOrEqual(t, d.Height, MaxImageDeliverySide)
}

func TestPrepareImageScalesAnExtremelyTallStripBelowTheUsualMinimumWidth(t *testing.T) {
	t.Parallel()
	data := gradientPNG(t, 200, 30000)
	d, err := PrepareImageForDelivery(data, "image/png")
	require.NoError(t, err)
	require.True(t, d.Preview)
	require.LessOrEqual(t, d.Height, MaxImageDeliverySide)
	require.Less(t, d.Width, previewMinWidth, "the width follows the height cap here")
	require.GreaterOrEqual(t, d.Width, 1)
}

func TestPrepareImageScalesAnExtremelyWideImage(t *testing.T) {
	t.Parallel()
	data := gradientPNG(t, 9000, 200)
	d, err := PrepareImageForDelivery(data, "image/png")
	require.NoError(t, err)
	require.True(t, d.Preview)
	require.LessOrEqual(t, d.Width, previewMaxWidth)
	require.LessOrEqual(t, d.Height, MaxImageDeliverySide)
}

func TestRepairOversizedImageOnlyTouchesOversizedImages(t *testing.T) {
	t.Parallel()
	normal := gradientPNG(t, 300, 200)
	got, mediaType := RepairOversizedImage(normal, "image/png")
	require.Equal(t, normal, got)
	require.Equal(t, "image/png", mediaType)

	got, mediaType = RepairOversizedImage([]byte("not an image"), "image/png")
	require.Equal(t, []byte("not an image"), got)
	require.Equal(t, "image/png", mediaType)

	tall := gradientPNG(t, 500, 8100)
	got, mediaType = RepairOversizedImage(tall, "image/png")
	require.Equal(t, "image/jpeg", mediaType)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(got))
	require.NoError(t, err)
	require.LessOrEqual(t, cfg.Height, MaxImageDeliverySide)
}

func TestEnforceImageDeliveryScalesATallToolImage(t *testing.T) {
	t.Parallel()
	resp := fantasy.NewImageResponse(gradientPNG(t, 1280, 9000), "image/png")
	out := EnforceImageDelivery(resp)
	require.Equal(t, "image", out.Type)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out.Data))
	require.NoError(t, err)
	require.LessOrEqual(t, cfg.Height, MaxImageDeliverySide)
	require.Contains(t, out.Content, "Preview delivered")
}

func TestPrepareImageRejectsCorruptAndEmptyData(t *testing.T) {
	t.Parallel()
	for name, data := range map[string][]byte{
		"empty":     nil,
		"text":      []byte("not an image"),
		"truncated": noisePNG(t, 32, 32)[:40],
	} {
		_, err := PrepareImageForDelivery(data, "image/png")
		require.ErrorIs(t, err, ErrImageInvalid, name)
	}
}

func TestPrepareImageRejectsDecompressionBombDimensions(t *testing.T) {
	t.Parallel()
	// A header claiming 100000×100000 pixels is refused before decoding.
	hdr := noisePNG(t, 1, 1)
	binary.BigEndian.PutUint32(hdr[16:], 100000)
	binary.BigEndian.PutUint32(hdr[20:], 100000)
	binary.BigEndian.PutUint32(hdr[29:], crc32.ChecksumIEEE(hdr[12:29]))
	_, err := PrepareImageForDelivery(hdr, "image/png")
	require.ErrorIs(t, err, ErrImageInvalid)
}

func TestViewDeliversAScreenshotLargerThan200KB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The case from the reported session: a 723,545 byte screenshot was
	// refused by the shared 200 KB text limit.
	data := paddedPNG(t, 1920, 1080, 723_545)
	path := filepath.Join(dir, "ekran.png")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	ctx := context.WithValue(context.Background(), SessionIDContextKey, "s")
	ctx = context.WithValue(ctx, SupportsImagesContextKey, true)
	resp := runViewTool(t, newViewToolForTest(dir), ctx, ViewParams{FilePath: path})
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, "image", resp.Type)
	require.Equal(t, data, resp.Data)
	require.Equal(t, "image/png", resp.MediaType)
	require.Contains(t, resp.Content, "not shown to the user")
	require.False(t, gjson.Get(resp.Metadata, "image_delivery.preview").Bool())
	require.EqualValues(t, 723_545, gjson.Get(resp.Metadata, "image_delivery.raw_bytes").Int())
}

func TestViewPreviewsAnImageOverTheLimitAndKeepsTheOriginal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data := paddedPNG(t, 1920, 1080, MaxImageDeliveryBytes+10)
	path := filepath.Join(dir, "big.png")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	ctx := context.WithValue(context.Background(), SessionIDContextKey, "s")
	ctx = context.WithValue(ctx, SupportsImagesContextKey, true)
	resp := runViewTool(t, newViewToolForTest(dir), ctx, ViewParams{FilePath: path})
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, "image/jpeg", resp.MediaType)
	require.LessOrEqual(t, len(resp.Data), MaxImageDeliveryBytes)
	require.True(t, gjson.Get(resp.Metadata, "image_delivery.preview").Bool())
	require.Contains(t, resp.Content, "over the 2097152 byte image limit")
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, onDisk)
}

func TestViewReportsACorruptImage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.png")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("a"), MaxViewSize+1), 0o644))
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "s")
	ctx = context.WithValue(ctx, SupportsImagesContextKey, true)
	resp := runViewTool(t, newViewToolForTest(dir), ctx, ViewParams{FilePath: path})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "corrupt or not a supported format")
}

func TestEnforceImageDeliveryKeepsStructuredTextAndMarksPreview(t *testing.T) {
	t.Parallel()
	data := paddedPNG(t, 1920, 1080, MaxImageDeliveryBytes+100)
	resp := fantasy.NewImageResponse(data, "image/png")
	resp.Content = `{"action":"observe","ok":true}`
	resp.Metadata = `{"atlas_outcome":{"dispatch":"none"}}`
	out := EnforceImageDelivery(resp)
	require.Equal(t, "image", out.Type)
	require.Equal(t, "image/jpeg", out.MediaType)
	require.LessOrEqual(t, len(out.Data), MaxImageDeliveryBytes)
	require.True(t, gjson.Get(out.Content, "ok").Bool(), "structured text stays parseable")
	require.Contains(t, gjson.Get(out.Content, "image_delivery_note").String(), "Preview delivered")
	require.Equal(t, "none", gjson.Get(out.Metadata, "atlas_outcome.dispatch").String())
	require.True(t, gjson.Get(out.Metadata, "image_delivery.preview").Bool())

	// An image within the limit passes through untouched.
	small := noisePNG(t, 300, 300)
	require.Greater(t, len(small), 200*1024)
	in := fantasy.NewImageResponse(small, "image/png")
	got := EnforceImageDelivery(in)
	require.Equal(t, small, got.Data)

	// A corrupt image is not sent, and the tool's text survives.
	bad := fantasy.NewImageResponse([]byte("nope"), "image/png")
	bad.Content = `{"ok":true}`
	gotBad := EnforceImageDelivery(bad)
	require.Equal(t, "text", gotBad.Type)
	require.Nil(t, gotBad.Data)
	require.True(t, gjson.Get(gotBad.Content, "ok").Bool())
	require.Contains(t, gjson.Get(gotBad.Content, "image_delivery_note").String(), "Image not delivered")
	require.False(t, gjson.Get(gotBad.Metadata, "image_delivery.delivered").Bool())

	// Text responses are untouched.
	text := fantasy.NewTextResponse("hello")
	require.Equal(t, text, EnforceImageDelivery(text))
}
