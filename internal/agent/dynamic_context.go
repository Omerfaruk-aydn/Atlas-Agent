package agent

import (
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

const dynamicContextMarker = "<runtime_context>"

// withDynamicContext adds request-scoped state after the stable conversation
// prefix. The block is intentionally not persisted: it is rebuilt from the
// authoritative session, work plan and runtime state before every request.
// Keeping it out of the provider system prompt also means changing a goal or
// completing a task does not invalidate the provider's cached prefix.
func withDynamicContext(messages []fantasy.Message, block string) []fantasy.Message {
	out := make([]fantasy.Message, 0, len(messages)+1)
	for _, msg := range messages {
		if isDynamicContextMessage(msg) {
			continue
		}
		out = append(out, msg)
	}
	if block = strings.TrimSpace(block); block != "" {
		out = append(out, fantasy.NewSystemMessage(dynamicContextMarker+"\n"+block+"\n</runtime_context>"))
	}
	return out
}

func isDynamicContextMessage(msg fantasy.Message) bool {
	if msg.Role != fantasy.MessageRoleSystem || len(msg.Content) != 1 {
		return false
	}
	text, ok := msg.Content[0].(fantasy.TextPart)
	return ok && strings.HasPrefix(text.Text, dynamicContextMarker+"\n")
}
