package mobile

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

type imageBackend struct {
	*fakeBackend
	media message.Message
}

func (b *imageBackend) ListMessages(context.Context, string) ([]message.Message, error) {
	return []message.Message{b.media}, nil
}

func TestChatImagesServeStoredMediaWithoutExposingPaths(t *testing.T) {
	g, backend := newGateway(t)
	var pngData bytes.Buffer
	require.NoError(t, png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	m := message.Message{ID: "picture", SessionID: "s1", Role: message.Tool, Parts: []message.ContentPart{
		message.ToolResult{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(pngData.Bytes())},
	}}
	g.opts.Backend = &imageBackend{fakeBackend: backend, media: m}
	phone := newPhone(t, g)
	resp, _ := phone.do("GET", "/api/sessions/s1/messages/picture/images/0", nil, nil)
	require.Equal(t, 401, resp.StatusCode, "stored images require a paired phone")
	view := messageView(m)
	require.Empty(t, view["text"])
	require.Len(t, view["images"], 1)
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	r.SetPathValue("id", "s1")
	r.SetPathValue("message", "picture")
	r.SetPathValue("index", "0")
	w := httptest.NewRecorder()
	g.handleMessageImage(w, r)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, pngData.Bytes(), w.Body.Bytes())
	r.SetPathValue("id", "child")
	w = httptest.NewRecorder()
	g.handleMessageImage(w, r)
	require.Equal(t, 404, w.Code)
	for _, p := range []message.ContentPart{
		message.ImageURLContent{URL: "https://example.org/private.png"},
		message.BinaryContent{Path: "C:/private.png", MIMEType: "image/png"},
		message.BinaryContent{Data: pngData.Bytes(), MIMEType: "image/jpeg"},
		message.ToolResult{Data: "not base64", MIMEType: "image/png"},
		message.BinaryContent{Data: make([]byte, maxChatImageBytes+1), MIMEType: "image/png"},
	} {
		data, _ := chatImage(p)
		require.Nil(t, data)
	}
}
