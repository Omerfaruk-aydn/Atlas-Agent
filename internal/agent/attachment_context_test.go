package agent

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

func TestLargeTextAttachmentBecomesPathReference(t *testing.T) {
	t.Parallel()
	a := &sessionAgent{}
	large := message.Attachment{FilePath: `D:\project\large.go`, MimeType: "text/plain", Content: []byte(strings.Repeat("package sample\n", 2000))}
	prompt := a.promptWithAttachments("inspect", []message.Attachment{large})
	require.Contains(t, prompt, "<file_reference")
	require.Contains(t, prompt, `path='D:\project\large.go'`)
	require.Contains(t, prompt, "size='30000'")
	require.Contains(t, prompt, "package sample")
	require.Less(t, len(prompt), 3000)
}

func TestSmallAndPastedAttachmentsKeepFullContent(t *testing.T) {
	t.Parallel()
	a := &sessionAgent{}
	small := message.Attachment{FilePath: "small.txt", MimeType: "text/plain", Content: []byte("complete small file")}
	pasted := message.Attachment{FileName: "paste_1.txt", FilePath: "paste_1.txt", MimeType: "text/plain", Content: []byte(strings.Repeat("user text", 3000))}
	prompt := a.promptWithAttachments("inspect", []message.Attachment{small, pasted})
	require.Contains(t, prompt, "<file path='small.txt'>\n\ncomplete small file")
	require.Contains(t, prompt, "<pasted_text")
	require.Contains(t, prompt, strings.Repeat("user text", 3000))
}

func TestHistoryReferencesDoNotMutateStoredAttachmentsOrMedia(t *testing.T) {
	t.Parallel()
	a := &sessionAgent{}
	large := []byte(strings.Repeat("history", 4000))
	image := []byte{1, 2, 3}
	m := message.Message{Role: message.User, Parts: []message.ContentPart{
		message.TextContent{Text: "inspect"},
		message.BinaryContent{Path: "large.txt", MIMEType: "text/plain", Data: large},
		message.BinaryContent{Path: "image.png", MIMEType: "image/png", Data: image},
	}}
	copyForModel := a.referenceLargeAttachments(m)
	require.Len(t, m.Parts[1].(message.BinaryContent).Data, len(large))
	require.Equal(t, image, copyForModel.Parts[1].(message.BinaryContent).Data)
	text := copyForModel.Content().Text
	require.Contains(t, text, "<file_reference")
	require.Contains(t, text, "size='28000'")
	require.Len(t, copyForModel.BinaryContent(), 1, "media remains a binary attachment")
}
