package export

import "regexp"

var (
	secretAssignments = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|auth[_-]?token|password|secret|authorization)(\s*["']?\s*[:=]\s*["']?)([^\s,"'}]+)`)
	bearerTokens      = regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/=-]+`)
	providerTokens    = regexp.MustCompile(`\b(?:sk|ghp|github_pat|xoxb|xoxp)-[A-Za-z0-9_-]{8,}`)
)

// RedactText masks credentials in free-form messages and tool output. Exported
// documents are local by design; masking is nevertheless mandatory because
// sharing the file is the common next step.
func RedactText(text string) string {
	text = secretAssignments.ReplaceAllString(text, `${1}${2}[REDACTED]`)
	text = bearerTokens.ReplaceAllString(text, `${1}[REDACTED]`)
	return providerTokens.ReplaceAllString(text, "[REDACTED]")
}
