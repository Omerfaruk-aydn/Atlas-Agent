package mobile

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	_ "golang.org/x/image/webp"
)

const maxChatImageBytes = 8 << 20

// chatImage accepts stored raster media only, never paths or external URLs.
func chatImage(part message.ContentPart) ([]byte, string) {
	var data []byte
	var mime, encoded string
	switch p := part.(type) {
	case message.BinaryContent:
		data, mime = p.Data, p.MIMEType
	case message.ToolResult:
		if p.IsError {
			return nil, ""
		}
		encoded, mime = p.Data, p.MIMEType
	case message.ImageURLContent:
		prefix, body, ok := strings.Cut(p.URL, ";base64,")
		if !ok || !strings.HasPrefix(prefix, "data:") {
			return nil, ""
		}
		mime, encoded = strings.TrimPrefix(prefix, "data:"), body
	default:
		return nil, ""
	}
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" && mime != "image/webp" {
		return nil, ""
	}
	if encoded != "" {
		if len(encoded) > base64.StdEncoding.EncodedLen(maxChatImageBytes) {
			return nil, ""
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, ""
		}
	}
	if len(data) == 0 || len(data) > maxChatImageBytes {
		return nil, ""
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return nil, ""
	}
	if "image/"+format != mime {
		return nil, ""
	}
	return data, mime
}

func messageImages(m message.Message) []map[string]any {
	var out []map[string]any
	for i, part := range m.Parts {
		if data, mime := chatImage(part); data != nil {
			out = append(out, map[string]any{"url": "/api/sessions/" + url.PathEscape(m.SessionID) + "/messages/" + url.PathEscape(m.ID) + "/images/" + strconv.Itoa(i), "mime_type": mime})
		}
	}
	return out
}

func (g *Gateway) handleMessageImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, err := g.opts.Backend.GetSession(r.Context(), id)
	if err != nil || s.ParentSessionID != "" {
		http.NotFound(w, r)
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		http.NotFound(w, r)
		return
	}
	msgs, err := g.opts.Backend.ListMessages(r.Context(), id)
	if err != nil {
		apiError(w, 500, "messages_failed", "could not load message")
		return
	}
	for _, m := range msgs {
		if m.ID != r.PathValue("message") || index >= len(m.Parts) {
			continue
		}
		data, mime := chatImage(m.Parts[index])
		if data == nil {
			break
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", "inline")
		_, _ = w.Write(data)
		return
	}
	http.NotFound(w, r)
}
