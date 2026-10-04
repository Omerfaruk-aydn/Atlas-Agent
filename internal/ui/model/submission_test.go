package model

import (
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

func TestFirstImagePromptAppearsBeforeAgentRunStarts(t *testing.T) {
	m := newBusyUI(&countingWorkspace{ready: true})
	warmCaches(m, false)
	cmd := m.sendMessage("What is in this image?", message.Attachment{FilePath: "photo.png", MimeType: "image/png", Content: []byte("image")})
	require.NotNil(t, cmd)
	// Do not execute the backend command: it may be uploading or preparing.
	require.Equal(t, 2, m.chat.list.Len(), "Prompt and working indicator must exist before the backend starts")
	rendered := ansi.Strip(m.chat.list.ItemAt(0).Render(100))
	require.Contains(t, rendered, "What is in this image?")
	require.Contains(t, rendered, "photo.png")
	require.Empty(t, m.sessionMessages, "Preview must not be persisted as conversation history")
	require.Contains(t, ansi.Strip(m.chat.list.ItemAt(1).Render(100)), "Waiting for response")

	m.setSessionMessages(nil)
	require.Equal(t, 2, m.chat.list.Len(), "An empty initial session snapshot must not erase feedback")
}

func TestFirstPromptFeedbackEndsOnSubmissionErrorOrCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		m := newBusyUI(&countingWorkspace{ready: true})
		warmCaches(m, false)
		m.sendMessage("hello")
		if cancel {
			m.doCancelAgent()
		} else {
			m.Update(agentRunSubmittedMsg{sessionID: "s1", previewID: m.firstPrompt.user.ID, err: errors.New("Upload failed")})
		}
		require.Nil(t, m.firstPrompt)
		require.Zero(t, m.chat.list.Len(), "Terminal paths must remove temporary messages and animation")
	}
}

func TestFirstPromptPreviewIsReplacedByAuthoritativeMessages(t *testing.T) {
	m := newBusyUI(&countingWorkspace{ready: true})
	warmCaches(m, false)
	m.sendMessage("hello")
	m.appendSessionMessage(message.Message{ID: "user", SessionID: "s1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}})
	require.Equal(t, 2, m.chat.list.Len(), "Confirmed user message must keep a working indicator")
	require.Equal(t, "user", m.chat.list.ItemAt(0).(interface{ ID() string }).ID())
	m.appendSessionMessage(m.sessionMessages[0])
	require.Equal(t, 2, m.chat.list.Len(), "Duplicate created events must retain feedback without duplicate prompts")
	m.setSessionMessages(m.sessionMessages)
	require.Equal(t, 2, m.chat.list.Len(), "Reloading the confirmed user prompt must keep feedback")
	m.appendSessionMessage(message.Message{ID: "assistant", SessionID: "s1", Role: message.Assistant})
	require.Equal(t, 2, m.chat.list.Len(), "Real assistant must replace the temporary working indicator")
	require.Len(t, m.sessionMessages, 2)
	require.Equal(t, "assistant", m.chat.list.ItemAt(1).(interface{ ID() string }).ID())
}

func TestFirstPromptFeedbackIsScopedToSessionAndIdleSubmission(t *testing.T) {
	pinTTLs(t)
	m := newBusyUI(&countingWorkspace{ready: true})
	warmCaches(m, true)
	m.sendMessage("queued")
	require.Nil(t, m.firstPrompt, "Queued prompts must not pretend to be active turns")
	require.Zero(t, m.chat.list.Len())
	warmCaches(m, false)
	m.sendMessage("hello")
	m.Update(agentRunSubmittedMsg{sessionID: "other", err: errors.New("Old request failed")})
	require.NotNil(t, m.firstPrompt, "Other session failures must not clear this submission")
	m.Update(loadSessionMsg{session: &session.Session{ID: "other"}})
	require.Nil(t, m.firstPrompt)
	require.Zero(t, m.chat.list.Len(), "Switching sessions must remove local feedback")
}

func TestNewChatDropsHistoryForFirstPromptFeedback(t *testing.T) {
	m := newBusyUI(&countingWorkspace{ready: true})
	warmCaches(m, false)
	m.appendSessionMessage(message.Message{ID: "old", SessionID: "s1", Role: message.User})
	m.newSession()
	require.Empty(t, m.sessionMessages)
	m.session = &session.Session{ID: "new"}
	warmCaches(m, false)
	m.sendMessage("new prompt")
	require.Equal(t, 2, m.chat.list.Len())
}
