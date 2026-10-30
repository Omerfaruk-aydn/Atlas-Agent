package model

import "strings"

const newTurnMarker = "[atlas:new-turn]\n"

// explicitTurnCommand gives users an unambiguous choice while an agent is
// working: steer folds into the active turn; queue starts an independent turn.
func explicitTurnCommand(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	for _, command := range []string{"/steer", "/queue"} {
		if trimmed != command && !strings.HasPrefix(trimmed, command+" ") {
			continue
		}
		message := strings.TrimSpace(strings.TrimPrefix(trimmed, command))
		if message == "" {
			return "", true
		}
		if command == "/queue" {
			return newTurnMarker + message, true
		}
		return "User steering for the active turn: " + message, true
	}
	return content, false
}
