package agent

import (
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
)

func (a *sessionAgent) promptWithAttachments(prompt string, attachments []message.Attachment) string {
	return message.PromptWithTextAttachmentsLimit(prompt, attachments, a.harnessOptions().AttachmentReferenceLimit())
}

// referenceLargeAttachments changes only the model-bound copy of a stored
// message. The original attachment remains intact in session persistence.
func (a *sessionAgent) referenceLargeAttachments(m message.Message) message.Message {
	limit := a.harnessOptions().AttachmentReferenceLimit()
	if limit <= 0 || m.Role != message.User {
		return m
	}
	parts := make([]message.ContentPart, 0, len(m.Parts))
	var references strings.Builder
	for _, part := range m.Parts {
		binary, ok := part.(message.BinaryContent)
		if !ok || binary.Path == "" || len(binary.Data) <= limit || !strings.HasPrefix(binary.MIMEType, "text/") {
			parts = append(parts, part)
			continue
		}
		preview := strings.ToValidUTF8(string(binary.Data[:min(len(binary.Data), 2048)]), "�")
		fmt.Fprintf(&references, "\n<file_reference path='%s' size='%d' note='Large attachment reduced to a preview; use the view tool to read the complete file.'>\n%s\n</file_reference>", binary.Path, len(binary.Data), preview)
	}
	if references.Len() > 0 {
		for i, part := range parts {
			if text, ok := part.(message.TextContent); ok {
				text.Text += references.String()
				parts[i] = text
				break
			}
		}
	}
	m.Parts = parts
	return m
}
