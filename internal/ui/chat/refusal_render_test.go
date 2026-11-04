package chat

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// TestContentFilterDoesNotAddBanner asserts that a provider content
// filter does not make the CLI invent a REFUSED banner. Any text the
// provider actually returned is the only text on the turn.
func TestContentFilterDoesNotAddBanner(t *testing.T) {
	sty := styles.AtlasPantera()
	msg := &message.Message{
		ID:   "refusal-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.Finish{
				Reason:  message.FinishReasonContentFilter,
				Message: "The request was rejected because it was considered high risk",
				Time:    testFinishTime,
			},
		},
	}

	require.False(t, msg.IsErrorLike())
	item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
	out := item.Render(80)
	require.NotContains(t, out, "REFUSED")
	require.NotContains(t, strings.ToLower(out), "safety classifier")
	require.NotContains(t, out, "high risk")
}

// TestErrorEmptyDetailsNoTrailingBlock locks in the behavior that an
// error finish with no details renders only the title line, without an
// empty styled details block or trailing blank lines.
func TestErrorEmptyDetailsNoTrailingBlock(t *testing.T) {
	sty := styles.AtlasPantera()
	msg := &message.Message{
		ID:   "error-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.Finish{
				Reason:  message.FinishReasonError,
				Message: "boom",
				Time:    testFinishTime,
			},
		},
	}
	item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
	out := item.Render(80)
	require.Contains(t, out, "ERROR")
	require.Contains(t, out, "boom")
	require.False(t, strings.HasSuffix(out, "\n"),
		"empty-details error must not render a trailing details block")
}
