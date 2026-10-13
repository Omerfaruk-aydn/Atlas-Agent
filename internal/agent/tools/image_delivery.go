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
