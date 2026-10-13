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
