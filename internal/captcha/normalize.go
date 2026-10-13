import (
	"strings"
	"unicode"
)

// NormalizeAnswer canonicalizes a typed answer for the target kind.
// Captcha text is case-insensitive on most portals; math answers are
// trimmed to a plain number.
func NormalizeAnswer(s string, kind Kind) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '\u00a0' {
			return -1
		}
		return r
	}, s)
	if kind == KindMath {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
