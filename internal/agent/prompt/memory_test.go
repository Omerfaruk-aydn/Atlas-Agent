package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildCoderPrompt renders the real coder template against a workspace, which
// is the only way to tell whether what the store holds actually reaches the
// model.
func buildCoderPrompt(t *testing.T, workingDir string) string {
	t.Helper()

	tpl := loadCoderTemplate(t)

	store := newPromptTestConfig(t, workingDir)

	p, err := NewPrompt("coder", string(tpl), WithWorkingDir(workingDir))
	require.NoError(t, err)

	out, err := p.Build(t.Context(), "openai", "gpt-5", store)
	require.NoError(t, err)
	return out
}

func loadCoderTemplate(t *testing.T) []byte {
	t.Helper()
	tpl, err := os.ReadFile(filepath.Join("..", "templates", "coder.md.tpl"))
	require.NoError(t, err)
	contract, err := os.ReadFile(filepath.Join("..", "templates", "agent_contract.md.tpl"))
	require.NoError(t, err)
	return append(tpl, contract...)
}

func writeMemory(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func TestBothStoresReachThePrompt(t *testing.T) {
	workingDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	writeMemory(t, filepath.Join(workingDir, ".atlas", "memory"), "MEMORY.md",
		"- the race detector needs a C compiler here\n")
	writeMemory(t, filepath.Join(configHome, "atlas"), "USER.md",
		"- answers in Turkish\n")

	out := buildCoderPrompt(t, workingDir)

	require.Contains(t, out, "the race detector needs a C compiler here")
	require.Contains(t, out, "answers in Turkish")
	require.Contains(t, out, "<project_memory>")
	require.Contains(t, out, "<user_memory>")
}

func TestEmptyStoresLeaveNoSection(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out := buildCoderPrompt(t, workingDir)

	require.NotContains(t, out, "<project_memory>")
	require.NotContains(t, out, "<user_memory>")
	require.NotContains(t, out, "# Memory")
}

// A write during a session lands on disk but must not change the prompt that
// session is already running with -- that prefix is what the provider caches.
func TestAWriteDoesNotChangeThePromptAlreadyBuilt(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	memoryDir := filepath.Join(workingDir, ".atlas", "memory")
	writeMemory(t, memoryDir, "MEMORY.md", "- first\n")

	before := buildCoderPrompt(t, workingDir)
	writeMemory(t, memoryDir, "MEMORY.md", "- first\n- second, written mid-session\n")

	require.Contains(t, before, "first")
	require.NotContains(t, before, "second, written mid-session",
		"the prompt is a snapshot; a later write is only visible to the next session")

	require.Contains(t, buildCoderPrompt(t, workingDir), "second, written mid-session",
		"but the next session does see it")
}

func TestOperatingConstraintsAppearWhenConfigured(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	tpl := loadCoderTemplate(t)

	store := newPromptTestConfig(t, workingDir)
	store.Config().Options.MaxSessionCost = 5
	store.Config().Options.MaxStepsPerTurn = 40
	store.Config().Options.AllowedDomains = []string{"docs.example.com"}
	store.Config().Options.BlockedDomains = []string{"internal.example.com"}

	p, err := NewPrompt("coder", string(tpl), WithWorkingDir(workingDir))
	require.NoError(t, err)
	out, err := p.Build(t.Context(), "openai", "gpt-5", store)
	require.NoError(t, err)

	require.Contains(t, out, "# Operating constraints")
	require.Contains(t, out, "capped at $5")
	require.Contains(t, out, "capped at 40 model/tool-call steps")
	require.Contains(t, out, "docs.example.com")
	require.Contains(t, out, "internal.example.com")
}

func TestOperatingConstraintsSectionOmittedWhenUnset(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out := buildCoderPrompt(t, workingDir)

	require.NotContains(t, out, "# Operating constraints")
}
