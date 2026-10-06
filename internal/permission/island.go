package permission

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
)

// Actions whose description would echo typed, secret or executable content.
var hiddenContentActions = map[string]bool{
	"type": true, "semantic_type": true, "set_value": true, "vault_fill": true,
	"auth_code": true, "eval": true, "cdp": true, "fill": true,
}

var (
	islandURL    = regexp.MustCompile(`https?://[^\s"'<>]+`)
	islandSecret = regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|api[_-]?key|apikey|auth|authorization|bearer|cookie|session)[\w-]*\s*[:=]\s*|bearer\s+|--(?:password|token|secret|api-key)[= ]\s*)((?:bearer|basic|token)\s+)?("[^"]*"|'[^']*'|\S+)`)
	islandOpaque = regexp.MustCompile(`\b[A-Za-z0-9_\-+=]{32,}\b`)
)

const islandDetailLimit = 320

// islandPrompt describes a pending request from the requesting tool's own
// fields. Typed text, scripts, URL queries and credential-like values are
// never shown on the desktop surface.
func islandPrompt(p PermissionRequest) activity.Prompt {
	detail := p.Description
	if hiddenContentActions[p.Action] {
		detail = ""
	}
	return activity.Prompt{
		Kind: activity.KindPermission,
		ID:   p.ID,
		Permission: activity.PromptPermission{
			Tool:      p.ToolName,
			Action:    p.Action,
			Detail:    RedactForDisplay(detail),
			Hidden:    hiddenContentActions[p.Action],
			Scope:     p.Path,
			Decisions: []activity.PermissionDecision{activity.DecisionAllowOnce, activity.DecisionAllowSession, activity.DecisionDeny},
		},
	}
}

// RedactForDisplay removes credential-like values and URL secrets and
// bounds the text for a compact surface.
func RedactForDisplay(text string) string {
	text = islandURL.ReplaceAllStringFunc(text, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "•••"
		}
		hidden := u.RawQuery != "" || u.Fragment != "" || u.User != nil
		u.RawQuery, u.Fragment, u.User = "", "", nil
		if hidden {
			return u.String() + "?•••"
		}
		return u.String()
	})
	text = islandSecret.ReplaceAllString(text, "${1}${2}•••")
	text = islandOpaque.ReplaceAllString(text, "•••")
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) > islandDetailLimit {
		runes := []rune(text)
		text = string(runes[:islandDetailLimit-1]) + "…"
	}
	return text
}
