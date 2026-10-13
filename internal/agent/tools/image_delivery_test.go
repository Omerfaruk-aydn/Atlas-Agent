
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
