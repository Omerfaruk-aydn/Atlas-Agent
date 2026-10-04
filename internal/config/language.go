package config

import (
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

// ValidateLanguage rejects unsupported interface languages before persistence.
func (c *Config) ValidateLanguage() error {
	if c.Options == nil || c.Options.TUI == nil || c.Options.TUI.Language == "" {
		return nil
	}
	if !i18n.Supported(c.Options.TUI.Language) {
		return fmt.Errorf("unsupported interface language %q; use en, tr, de, fr, it or ar", c.Options.TUI.Language)
	}
	return nil
}
