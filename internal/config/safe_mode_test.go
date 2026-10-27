package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSafeDoesNotExecuteShellConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	marker := filepath.Join(root, "shell-executed")
	require.NoError(t, os.WriteFile(filepath.Join(root, "atlasrc"), []byte("touch \""+marker+"\"\n"), 0o644))
	store, err := LoadSafe(root, filepath.Join(root, ".atlas"), false)
	require.NoError(t, err)
	require.True(t, store.Config().Options.SafeMode)
	_, err = os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestApplySafeModeRetainsOnlyProviderFoundation(t *testing.T) {
	cfg := &Config{Options: &Options{ContextPaths: []string{"AGENTS.md"}, SkillsPaths: []string{"skills"}, OutputStyle: "learning", UsageProfile: "research"}, Hooks: map[string][]HookConfig{"PreToolUse": {{Command: "touch no"}}}, MCP: MCPs{"custom": {}}, LSP: LSPs{"custom": {}}, Env: map[string]string{"A": "B"}}
	cfg.applySafeMode()
	require.True(t, cfg.Options.SafeMode)
	require.Empty(t, cfg.Hooks)
	require.Empty(t, cfg.MCP)
	require.Empty(t, cfg.LSP)
	require.Empty(t, cfg.Env)
	require.Empty(t, cfg.Options.ContextPaths)
	require.Empty(t, cfg.Options.SkillsPaths)
	require.Empty(t, cfg.Options.OutputStyle)
	require.Empty(t, cfg.Options.UsageProfile)
}
