package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOutputStyles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"default", "concise", "explanatory", "learning"} {
		text, err := outputStyle(&config.Config{Options: &config.Options{OutputStyle: name}}, root)
		require.NoError(t, err)
		require.NotEmpty(t, text)
	}
}

func TestOutputStyleProjectFileAndSafeMode(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(root, ".atlas", "output-styles")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "review.md"), []byte("Lead with findings and cite exact lines."), 0o644))
	cfg := &config.Config{Options: &config.Options{OutputStyle: "review"}}
	text, err := outputStyle(cfg, root)
	require.NoError(t, err)
	require.Equal(t, "Lead with findings and cite exact lines.", text)
	cfg.Options.SafeMode = true
	text, err = outputStyle(cfg, root)
	require.NoError(t, err)
	require.Empty(t, text)
}

func TestOutputStyleValidation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../secret", "missing", "UPPER CASE"} {
		_, err := outputStyle(&config.Config{Options: &config.Options{OutputStyle: name}}, root)
		require.Error(t, err)
	}
	dir := filepath.Join(root, ".atlas", "output-styles")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "large.md"), []byte(strings.Repeat("x", maxOutputStyleBytes+1)), 0o644))
	_, err := outputStyle(&config.Config{Options: &config.Options{OutputStyle: "large"}}, root)
	require.ErrorContains(t, err, "16 KiB")
}
