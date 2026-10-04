package model

import (
	"time"

	"github.com/google/uuid"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/chat"
)

// firstPromptPreview covers the gap before the first persisted messages arrive.
// Attachment metadata is enough for feedback; image bytes stay in AgentRun.
type firstPromptPreview struct {
	user      message.Message
	waiting   *chat.SubmissionItem
	confirmed bool
}

func (m *UI) previewFirstPrompt(content string, attachments []message.Attachment) tea.Cmd {
	if m.firstPrompt != nil || len(m.sessionMessages) != 0 || m.isAgentBusy() {
		return nil
	}
	id := "submission-" + uuid.NewString()
	parts := []message.ContentPart{message.TextContent{Text: content}}
	for _, attachment := range attachments {
		parts = append(parts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType})
	}
	m.firstPrompt = &firstPromptPreview{
		user:    message.Message{ID: id, SessionID: m.session.ID, Role: message.User, CreatedAt: time.Now().Unix(), Parts: parts},
		waiting: chat.NewSubmissionItem(m.com.Styles, id+"-waiting"),
	}
	return m.restoreFirstPromptPreview()
}

func (m *UI) restoreFirstPromptPreview() tea.Cmd {
	p := m.firstPrompt
	if p == nil || p.user.SessionID != m.currentSessionID() {
		return nil
	}
	if !p.confirmed && m.chat.MessageItem(p.user.ID) == nil {
		m.chat.AppendMessages(chat.ExtractMessageItems(m.com.Styles, &p.user, nil, m.com.Workspace.WorkingDir(), m.caps)...)
	}
	if m.chat.MessageItem(p.waiting.ID()) == nil {
		m.chat.AppendMessages(p.waiting)
	}
	return tea.Batch(p.waiting.StartAnimation(), m.chat.ScrollToBottomAndAnimate())
}

func (m *UI) reconcileFirstPrompt(msg message.Message) {
	p := m.firstPrompt
	if p == nil || msg.SessionID != p.user.SessionID {
		return
	}
	switch msg.Role {
	case message.User:
		p.confirmed = true
		m.chat.RemoveMessage(p.user.ID)
		m.chat.RemoveMessage(p.waiting.ID())
	case message.Assistant:
		m.clearFirstPromptPreview()
	}
}

func (m *UI) clearFirstPromptPreview() {
	if p := m.firstPrompt; p != nil {
		m.chat.RemoveMessage(p.user.ID)
		m.chat.RemoveMessage(p.waiting.ID())
		m.firstPrompt = nil
	}
}
