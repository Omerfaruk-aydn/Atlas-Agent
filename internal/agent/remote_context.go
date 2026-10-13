package agent

import (
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

const remoteContextMarker = "<atlas_remote_context>"

// remoteContextBlock is the live /support target, or "" when none.
func (a *sessionAgent) remoteContextBlock() string {
	if a.remoteContext == nil {
		return ""
	}
	return a.remoteContext()
}

// withRemoteContext replaces any earlier remote context block with the
// current one. It is regenerated every step instead of living in history,
// so a summary or a resumed task can never carry a stale device, session
// or generation.
func withRemoteContext(messages []fantasy.Message, block string) []fantasy.Message {
	result := make([]fantasy.Message, 0, len(messages)+1)
	for _, msg := range messages {
		if msg.Role == fantasy.MessageRoleSystem && len(msg.Content) == 1 {
			if text, ok := msg.Content[0].(fantasy.TextPart); ok && strings.HasPrefix(text.Text, remoteContextMarker+"\n") {
				continue
			}
		}
		result = append(result, msg)
	}
	if block != "" {
		result = append(result, fantasy.NewSystemMessage(remoteContextMarker+"\n"+block+"\n</atlas_remote_context>"))
	}
	return result
}
