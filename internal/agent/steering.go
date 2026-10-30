package agent

import (
	"strings"

	"github.com/google/uuid"
)

const newTurnMarker = "[atlas:new-turn]\n"

// applyTurnRouting makes the UI's explicit queue choice visible to the queue
// drain: RunID-bearing prompts remain independent turns, while ordinary
// follow-ups fold into the next step as user steering.
func applyTurnRouting(call SessionAgentCall) SessionAgentCall {
	if strings.HasPrefix(call.Prompt, newTurnMarker) {
		call.Prompt = strings.TrimPrefix(call.Prompt, newTurnMarker)
		if call.RunID == "" {
			call.RunID = uuid.NewString()
		}
	}
	return call
}
