}

func redactPart(sessionID string, part fantasy.MessagePart) fantasy.MessagePart {
	switch p := part.(type) {
	case fantasy.TextPart:
		p.Text = vault.Redact(sessionID, p.Text)
		return p
	case *fantasy.TextPart:
		c := *p
		c.Text = vault.Redact(sessionID, c.Text)
		return c
	case fantasy.ToolCallPart:
		p.Input = vault.Redact(sessionID, p.Input)
		return p
	case *fantasy.ToolCallPart:
		c := *p
		c.Input = vault.Redact(sessionID, c.Input)
		return c
	case fantasy.ToolResultPart:
		p.Output = redactOutput(sessionID, p.Output)
		return p
	case *fantasy.ToolResultPart:
		c := *p
		c.Output = redactOutput(sessionID, c.Output)
		return c
	}
	return part
}

func redactOutput(sessionID string, out fantasy.ToolResultOutputContent) fantasy.ToolResultOutputContent {
	switch o := out.(type) {
	case fantasy.ToolResultOutputContentText:
		o.Text = vault.Redact(sessionID, o.Text)
		return o
	case *fantasy.ToolResultOutputContentText:
		c := *o
		c.Text = vault.Redact(sessionID, c.Text)
		return c
	case fantasy.ToolResultOutputContentError:
		if o.Error != nil {
			o.Error = errors.New(vault.Redact(sessionID, o.Error.Error()))
		}
		return o
	case fantasy.ToolResultOutputContentMedia:
		o.Text = vault.Redact(sessionID, o.Text)
		return o
	}
	return out
}

// redactStoredToolCall rewrites the persisted arguments of one tool call
// after its tool registered a secret (for example a refused password), so
// the conversation record and later requests never keep that value. It
// reports whether anything changed.
func redactStoredToolCall(msg *message.Message, toolCallID string) bool {
	if msg == nil || !vault.Sensitive(msg.SessionID) {
		return false
	}
	changed := false
	for i, part := range msg.Parts {
		tc, ok := part.(message.ToolCall)
		if !ok || tc.ID != toolCallID {
			continue
		}
		if redacted := vault.Redact(msg.SessionID, tc.Input); redacted != tc.Input {
			tc.Input = redacted
			msg.Parts[i] = tc
			changed = true
		}
	}
	return changed
}
