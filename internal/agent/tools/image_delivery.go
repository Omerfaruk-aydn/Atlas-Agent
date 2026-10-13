		}
		width = max(previewMinWidth, width*3/4)
	}
}

// boxScale averages each destination pixel's source box, keeping text
// strokes legible instead of dropping them between samples.
func boxScale(img image.Image, dstW, dstH int) *image.RGBA {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	// RGBA() supplies premultiplied channels; keep that representation so
	// transparent edges are not multiplied by alpha a second time.
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for dy := range dstH {
		y0 := dy * srcH / dstH
		y1 := max(y0+1, (dy+1)*srcH/dstH)
		for dx := range dstW {
			x0 := dx * srcW / dstW
			x1 := max(x0+1, (dx+1)*srcW/dstW)
			var r, g, bl, a, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, a, n = r+cr>>8, g+cg>>8, bl+cb>>8, a+ca>>8, n+1
				}
			}
			i := dst.PixOffset(dx, dy)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n), uint8(g/n), uint8(bl/n), uint8(a/n)
		}
	}
	return dst
}

// EnforceImageDelivery applies the delivery contract to any tool response
// that carries an image. Text responses pass through unchanged. An image
// that cannot be delivered becomes a text response that says so; the text
// the tool produced is kept so a structured result is never lost. When the
// tool's text is a JSON object the note is added as a field, so callers
// that parse it keep working.
func EnforceImageDelivery(resp fantasy.ToolResponse) fantasy.ToolResponse {
	if resp.Type != "image" || len(resp.Data) == 0 {
		return resp
	}
	d, err := PrepareImageForDelivery(resp.Data, resp.MediaType)
	if err != nil {
		out := resp
		out.Type, out.Data, out.MediaType = "text", nil, ""
		out.Content = withDeliveryNote(resp.Content, "Image not delivered: "+err.Error())
		out.Metadata = withImageDeliveryMeta(resp.Metadata, map[string]any{"delivered": false, "error": err.Error()})
		return out
	}
	var previous struct {
		Delivery ImageDelivery `json:"image_delivery"`
	}
	if json.Unmarshal([]byte(resp.Metadata), &previous) == nil {
		old := previous.Delivery
		// Preserve a prior validated transform only when it describes these
		// exact delivered bytes and dimensions. Repeated wrapping is inert.
		if old.Preview && old.SHA256 == d.SHA256 && old.Width == d.Width && old.Height == d.Height && old.OriginalWidth > 0 && old.OriginalHeight > 0 {
			d = old
			d.Data, d.Recipient = resp.Data, "model"
		}
	}
	resp.Data, resp.MediaType = d.Data, d.MediaType
	if d.Preview {
		resp.Content = withDeliveryNote(resp.Content, d.Note())
	}
	resp.Metadata = withImageDeliveryMeta(resp.Metadata, d)
	return resp
}

func withDeliveryNote(content, note string) string {
	if strings.Contains(content, note) {
		return content
	}
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		if out, err := sjson.Set(content, "image_delivery_note", note); err == nil {
			return out
		}
	}
	if content == "" {
		return note
	}
	return content + "\n\n" + note
}

// WithImageDelivery wraps a tool so every image it returns obeys the
// delivery contract, whichever tool produced it (view, computer,
// remote_support, browser, MCP).
func WithImageDelivery(tool fantasy.AgentTool) fantasy.AgentTool {
	return &imageDeliveryTool{tool}
}

type imageDeliveryTool struct{ fantasy.AgentTool }

func (t *imageDeliveryTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	r, err := t.AgentTool.Run(ctx, call)
	if err != nil {
		return r, err
	}
	return EnforceImageDelivery(r), nil
}

func withImageDeliveryMeta(meta string, value any) string {
	if meta == "" {
		meta = "{}"
	}
	out, err := sjson.Set(meta, "image_delivery", value)
	if err != nil {
		// Metadata that is not a JSON object is left untouched.
		return meta
	}
	return out
}
