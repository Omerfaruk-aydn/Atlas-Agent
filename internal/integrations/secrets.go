package integrations

import (
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Secrets redacts registered secret values and common credential shapes
// from any text before it is stored, shown or handed to the model.
type Secrets struct {
	mu     sync.RWMutex
	values []string
}

// minSecretLen keeps short values (ports, "true") from being registered and
// then blanking unrelated text.
const minSecretLen = 6

// Add registers resolved secret values.
func (s *Secrets) Add(values ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range values {
		v = strings.TrimSpace(v)
		if len(v) < minSecretLen || slices.Contains(s.values, v) {
			continue
		}
		s.values = append(s.values, v)
	}
	// Longest first so a secret containing another is fully replaced.
	slices.SortFunc(s.values, func(a, b string) int { return len(b) - len(a) })
}

var credentialPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), "$1 •••"},
	{regexp.MustCompile(`(?i)([?&;\s"']?(?:api[_-]?key|key|token|access[_-]?token|refresh[_-]?token|secret|password|passwd|pwd|auth|signature|sig)\s*[=:]\s*"?)[^&\s"',;]+`), "${1}•••"},
	{regexp.MustCompile(`\b(sk|pk|rk|ghp|gho|ghu|ghs|github_pat|xox[abpr]|AIza|ya29|ABSK|vck)[-_.][A-Za-z0-9._-]{8,}`), "•••"},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{6,}`), "•••"},
}

// Redact removes registered secrets and credential-shaped substrings.
func (s *Secrets) Redact(text string) string {
	if text == "" {
		return text
	}
	if s != nil {
		s.mu.RLock()
		for _, v := range s.values {
			text = strings.ReplaceAll(text, v, "•••")
		}
		s.mu.RUnlock()
	}
	for _, p := range credentialPatterns {
		text = p.re.ReplaceAllString(text, p.repl)
	}
	return text
}
