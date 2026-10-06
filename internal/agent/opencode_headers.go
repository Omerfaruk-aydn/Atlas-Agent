package agent

import (
	"strings"

	"github.com/google/uuid"
)

// OpenCode requires the coding agent's own identity and stable session affinity.
// Per-call conversation headers override this provider-lifetime fallback.
func openCodeHeaders(headers map[string]string) map[string]string {
	sessionID := ""
	for key, value := range headers {
		switch {
		case strings.EqualFold(key, "User-Agent"):
			delete(headers, key)
		case strings.EqualFold(key, "x-opencode-session"):
			if strings.TrimSpace(value) != "" {
				sessionID = value
			}
			delete(headers, key)
		}
	}
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	headers["User-Agent"] = userAgent
	headers["x-opencode-session"] = sessionID
	return headers
}
