package message

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func makeTestAttachments(n int, contentSize int) []Attachment {
	attachments := make([]Attachment, n)
	content := []byte(strings.Repeat("x", contentSize))
	for i := range n {
		attachments[i] = Attachment{
			FilePath: fmt.Sprintf("/path/to/file%d.txt", i),
			MimeType: "text/plain",
			Content:  content,
		}
	}
	return attachments
}

func TestToAIMessage_CorruptedMediaData(t *testing.T) {
	t.Parallel()

	msg := &Message{
		Role: Tool,
		Parts: []ContentPart{
			ToolResult{
				ToolCallID: "call_123",
				Name:       "screenshot",
				Content:    "Loaded image/png content",
				Data:       "abc\x80def",
				MIMEType:   "image/png",
			},
		},
	}

	messages := msg.ToAIMessage()
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 1)

	part, ok := messages[0].Content[0].(fantasy.ToolResultPart)
	require.True(t, ok)

	require.Equal(t, "call_123", part.ToolCallID)

	textContent, ok := part.Output.(fantasy.ToolResultOutputContentText)
	require.True(t, ok, "corrupted media should be downgraded to text")
	require.Equal(t, "Loaded image/png content\n\n"+mediaLoadFailedPlaceholder, textContent.Text)
}

func TestToAIMessage_ValidMediaData(t *testing.T) {
	t.Parallel()

	validBase64 := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47})

	msg := &Message{
		Role: Tool,
		Parts: []ContentPart{
			ToolResult{
				ToolCallID: "call_456",
				Name:       "screenshot",
				Content:    "Loaded image/png content",
				Data:       validBase64,
				MIMEType:   "image/png",
			},
		},
	}

	messages := msg.ToAIMessage()
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 1)

	part, ok := messages[0].Content[0].(fantasy.ToolResultPart)
	require.True(t, ok)

	require.Equal(t, "call_456", part.ToolCallID)

	mediaContent, ok := part.Output.(fantasy.ToolResultOutputContentMedia)
	require.True(t, ok, "valid media should remain as media")
	require.Equal(t, validBase64, mediaContent.Data)
	require.Equal(t, "image/png", mediaContent.MediaType)
	require.Equal(t, "Loaded image/png content", mediaContent.Text)
}

func TestToAIMessage_ASCIIButInvalidBase64(t *testing.T) {
	t.Parallel()

	msg := &Message{
		Role: Tool,
		Parts: []ContentPart{
			ToolResult{
				ToolCallID: "call_789",
				Name:       "screenshot",
				Content:    "Loaded image/png content",
				Data:       "not-valid-base64!!!",
				MIMEType:   "image/png",
			},
		},
	}

	messages := msg.ToAIMessage()
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 1)

	part, ok := messages[0].Content[0].(fantasy.ToolResultPart)
	require.True(t, ok)

	require.Equal(t, "call_789", part.ToolCallID)

	textContent, ok := part.Output.(fantasy.ToolResultOutputContentText)
	require.True(t, ok, "ASCII but invalid base64 should be downgraded to text")
	require.Equal(t, "Loaded image/png content\n\n"+mediaLoadFailedPlaceholder, textContent.Text)
}

func TestPersistedScreenshotRetainsViewportTextWhenReloaded(t *testing.T) {
	t.Parallel()
	svc, sessionID := newTestService(t)
	caption := `Viewport: {"viewport_id":"vp-persisted","image_width":1600,"image_height":765,"image_scale":0.8333333333333334}`
	data := base64.StdEncoding.EncodeToString([]byte("image bytes"))
	created, err := svc.Create(t.Context(), sessionID, CreateMessageParams{
		Role:  Tool,
		Parts: []ContentPart{ToolResult{ToolCallID: "screenshot-call", Name: "browser", Content: caption, Data: data, MIMEType: "image/png"}},
	})
	require.NoError(t, err)
	reloaded, err := svc.Get(t.Context(), created.ID)
	require.NoError(t, err)
	messages := reloaded.ToAIMessage()
	require.Len(t, messages, 1)
	part, ok := messages[0].Content[0].(fantasy.ToolResultPart)
	require.True(t, ok)
	media, ok := part.Output.(fantasy.ToolResultOutputContentMedia)
	require.True(t, ok)
	require.Equal(t, caption, media.Text)
	require.Equal(t, data, media.Data)
	require.Equal(t, "screenshot-call", part.ToolCallID)
}

func BenchmarkPromptWithTextAttachments(b *testing.B) {
	cases := []struct {
		name        string
		numFiles    int
		contentSize int
	}{
		{"1file_100bytes", 1, 100},
		{"5files_1KB", 5, 1024},
		{"10files_10KB", 10, 10 * 1024},
		{"20files_50KB", 20, 50 * 1024},
	}

	for _, tc := range cases {
		attachments := makeTestAttachments(tc.numFiles, tc.contentSize)
		prompt := "Process these files"

		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = PromptWithTextAttachments(prompt, attachments)
			}
		})
	}
}

func TestResetStreamedContent(t *testing.T) {
	t.Parallel()

	msg := &Message{}
	msg.AddImageURL("https://example.com/img.png", "high")
	msg.AppendContent("partial answer")
	msg.AppendReasoningContent("thinking...")
	msg.AddToolCall(ToolCall{ID: "1", Name: "bash"})
	msg.AddToolResult(ToolResult{ToolCallID: "1", Content: "output"})
	msg.AddFinish(FinishReasonError, "boom", "stream died")

	msg.ResetStreamedContent()

	// Streamed parts are gone.
	require.Empty(t, msg.Content().Text, "text should be cleared")
	require.Empty(t, msg.ReasoningContent().Thinking, "reasoning should be cleared")
	require.Empty(t, msg.ToolCalls(), "tool calls should be cleared")
	require.Nil(t, msg.FinishPart(), "finish should be cleared")

	// Non-streamed parts survive.
	require.Len(t, msg.ImageURLContent(), 1, "image should survive")
	require.Len(t, msg.ToolResults(), 1, "tool results should survive")
}

func TestResetStreamedContentEmpty(t *testing.T) {
	t.Parallel()

	// Reset on an empty message is a no-op and must not panic.
	msg := &Message{}
	msg.ResetStreamedContent()
	require.Empty(t, msg.Parts)
}

func TestPromptWithTextAttachmentsMarksPastedText(t *testing.T) {
	t.Parallel()
	prompt := PromptWithTextAttachments("look", []Attachment{
		{FileName: "paste_1.txt", FilePath: "paste_1.txt", MimeType: "text/plain", Content: []byte("pasted brief")},
		{FileName: "main.go", FilePath: "src/main.go", MimeType: "text/plain", Content: []byte("package main")},
	})
	require.Contains(t, prompt, "<pasted_text note='the user pasted this as part of the message; it is not a file on disk'>\npasted brief\n</pasted_text>")
	require.NotContains(t, prompt, "<file path='paste_1.txt'>")
	require.Contains(t, prompt, "<file path='src/main.go'>")
}
