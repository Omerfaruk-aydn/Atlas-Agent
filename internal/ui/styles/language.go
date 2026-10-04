package styles

import (
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

// RelativeTime formats an interface timestamp in this style's locale.
func (s *Styles) RelativeTime(at time.Time) string {
	return i18n.RelativeTime(s.Language(), at, time.Now())
}

// Text translates an Atlas-authored literal using this interface's language.
func (s *Styles) Text(source string) string {
	if s == nil {
		return source
	}
	return s.Locale.Text(source)
}

// Language returns the active language or English for uninitialized styles.
func (s *Styles) Language() string {
	if s == nil {
		return "en"
	}
	return s.Locale.Code()
}
