package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLanguageValidation(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"", "en", "tr", "de", "fr", "it", "ar"} {
		cfg := Config{Options: &Options{TUI: &TUIOptions{Language: code}}}
		require.NoError(t, cfg.ValidateLanguage())
	}
	cfg := Config{Options: &Options{TUI: &TUIOptions{Language: "xx"}}}
	require.Error(t, cfg.ValidateLanguage())
}

func TestLanguageDiskPersistenceAndRejectedWrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "atlas.json")
	store := &ConfigStore{config: &Config{}, globalDataPath: path}
	require.NoError(t, store.SetConfigField(ScopeGlobal, "options.tui.language", "ar"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	reloaded, err := loadFromBytes([][]byte{data})
	require.NoError(t, err)
	require.Equal(t, "ar", reloaded.Options.TUI.Language)
	require.Error(t, store.SetConfigField(ScopeGlobal, "options.tui.language", "xx"))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, after, "Rejected locale must not alter the saved preference")
}
