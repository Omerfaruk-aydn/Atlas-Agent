package export

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestHTMLIsSelfContainedEscapedAndRedacted(t *testing.T) {
	t.Parallel()
	sess := session.Session{Title: `<script>alert("x")</script>`, Cost: 1.25}
	messages := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "api_key=sk-abcdefghijkl\nBearer very-secret-token"}}}}
	document := HTML(sess, messages)
	require.NotContains(t, document, "<script>alert")
	require.Contains(t, document, "&lt;script&gt;")
	require.NotContains(t, document, "sk-abcdefghijkl")
	require.NotContains(t, document, "very-secret-token")
	require.Contains(t, document, "1.250000 cost")
	require.NotContains(t, document, "<script src=")
}

func TestMarkdownAndJSONRedactCredentials(t *testing.T) {
	t.Parallel()
	sess := session.Session{Title: "Export"}
	messages := []message.Message{{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "password=hunter2"}}}}
	require.NotContains(t, Markdown(sess, messages), "hunter2")
	data, err := JSON(sess, messages)
	require.NoError(t, err)
	require.NotContains(t, string(data), "hunter2")
}
