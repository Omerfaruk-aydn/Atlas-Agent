// Package export renders a session's conversation as portable Markdown, for
// saving, sharing, or reading outside the TUI.
package export

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

// maxRenderedLen truncates a tool call's input or a tool result's content
// past this many characters. A full-file read or a large diff dumped
// verbatim would otherwise make the export about the tool traffic rather
// than the conversation; the point of an export is what was said and
// decided, not a byte-for-byte replay.
const maxRenderedLen = 2000

// Markdown renders sess's messages as a single Markdown document.
//
// Reasoning content and step-finish bookkeeping are left out: they are
// provider-internal detail with nothing for a reader outside the session to
// act on. Tool calls and their results are kept but truncated, since a
// conversation with tool use is still readable through them, unlike through
// the raw content they touched.
func Markdown(sess session.Session, msgs []message.Message) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", nonEmpty(sess.Title, "Untitled session"))
	fmt.Fprintf(&b, "- Exported: %s\n", time.Now().UTC().Format(time.RFC3339))
	if sess.CreatedAt > 0 {
		fmt.Fprintf(&b, "- Started: %s\n", time.Unix(sess.CreatedAt, 0).UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "- Messages: %d\n\n", len(msgs))
	b.WriteString("---\n\n")

	for _, msg := range msgs {
		renderMessage(&b, msg)
	}

	return RedactText(b.String())
}

// JSONMessage is one message in a JSON export. It flattens the parts a
// caller is likely to want out of the conversation -- text, tool calls, and
// tool results -- rather than round-tripping the internal ContentPart
// union, which is not something an external consumer should have to decode.
type JSONMessage struct {
	Role        string           `json:"role"`
	Model       string           `json:"model,omitempty"`
	CreatedAt   int64            `json:"created_at,omitempty"`
	Text        string           `json:"text,omitempty"`
	ToolCalls   []JSONToolCall   `json:"tool_calls,omitempty"`
	ToolResults []JSONToolResult `json:"tool_results,omitempty"`
}

type JSONToolCall struct {
	Name  string `json:"name"`
	Input string `json:"input,omitempty"`
}

type JSONToolResult struct {
	Name    string `json:"name,omitempty"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// JSONExport is the top-level document JSON renders.
type JSONExport struct {
	Title     string        `json:"title"`
	CreatedAt int64         `json:"created_at,omitempty"`
	Messages  []JSONMessage `json:"messages"`
}

// JSON renders sess's messages as a machine-readable document. Tool input
// and results are truncated the same way Markdown truncates them: a full
// dump would make the export about the tool traffic rather than the
// conversation.
func JSON(sess session.Session, msgs []message.Message) ([]byte, error) {
	doc := JSONExport{
		Title:     nonEmpty(sess.Title, "Untitled session"),
		CreatedAt: sess.CreatedAt,
		Messages:  make([]JSONMessage, 0, len(msgs)),
	}

	for _, msg := range msgs {
		jm := JSONMessage{
			Role:      string(msg.Role),
			Model:     msg.Model,
			CreatedAt: msg.CreatedAt,
			Text:      strings.TrimSpace(msg.Content().Text),
		}
		for _, tc := range msg.ToolCalls() {
			jm.ToolCalls = append(jm.ToolCalls, JSONToolCall{Name: tc.Name, Input: truncate(tc.Input)})
		}
		for _, tr := range msg.ToolResults() {
			jm.ToolResults = append(jm.ToolResults, JSONToolResult{
				Name:    tr.Name,
				Content: truncate(tr.Content),
				IsError: tr.IsError,
			})
		}
		doc.Messages = append(doc.Messages, jm)
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	return []byte(RedactText(string(data))), err
}

func renderMessage(b *strings.Builder, msg message.Message) {
	heading := roleHeading(msg.Role)
	fmt.Fprintf(b, "## %s\n\n", heading)

	wrote := false
	for _, part := range msg.Parts {
		if renderPart(b, part) {
			wrote = true
		}
	}
	if !wrote {
		b.WriteString("_(no content)_\n\n")
	}
}

// renderPart writes one content part and reports whether it wrote anything
// visible -- ReasoningContent and Finish return false, so an assistant turn
// that was only internal bookkeeping still shows the "(no content)" marker
// rather than a silently empty section.
func renderPart(b *strings.Builder, part message.ContentPart) bool {
	switch p := part.(type) {
	case message.TextContent:
		if strings.TrimSpace(p.Text) == "" {
			return false
		}
		b.WriteString(p.Text)
		b.WriteString("\n\n")
		return true

	case message.ToolCall:
		fmt.Fprintf(b, "**Tool call: `%s`**\n\n", p.Name)
		if input := strings.TrimSpace(p.Input); input != "" {
			fmt.Fprintf(b, "```\n%s\n```\n\n", truncate(input))
		}
		return true

	case message.ToolResult:
		label := "Tool result"
		if p.IsError {
			label = "Tool error"
		}
		fmt.Fprintf(b, "**%s:**\n\n", label)
		fmt.Fprintf(b, "```\n%s\n```\n\n", truncate(p.Content))
		return true

	case message.ShellCommand:
		fmt.Fprintf(b, "```bash\n%s\n```\n\n", p.Command)
		if output := strings.TrimSpace(p.Output); output != "" {
			fmt.Fprintf(b, "```\n%s\n```\n\n", truncate(output))
		}
		return true

	default:
		// ReasoningContent, Finish, ImageURLContent, BinaryContent: not
		// meaningful outside the session that produced them.
		return false
	}
}

func roleHeading(role message.MessageRole) string {
	switch role {
	case message.User:
		return "User"
	case message.Assistant:
		return "Assistant"
	case message.System:
		return "System"
	case message.Tool:
		return "Tool"
	default:
		return string(role)
	}
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxRenderedLen {
		return s
	}
	return s[:maxRenderedLen] + fmt.Sprintf("\n… truncated (%d bytes total)", len(s))
}

func nonEmpty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
