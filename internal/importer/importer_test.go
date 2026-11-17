package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/plugin"
	"github.com/stretchr/testify/require"
)

func put(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestScanClaude(t *testing.T) {
	t.Parallel()
	home, project := t.TempDir(), t.TempDir()
	put(t, filepath.Join(home, ".claude", "commands", "review.md"), "---\ndescription: Review a diff\n---\nLook at the diff.\n")
	put(t, filepath.Join(project, ".claude", "agents", "tester.md"), "You write tests.\n")
	put(t, filepath.Join(home, ".claude", "skills", "deploy", "SKILL.md"), "---\nname: deploy\ndescription: Ship it\n---\nSteps.\n")
	put(t, filepath.Join(home, ".claude", "CLAUDE.md"), "# Rules\nBe brief.\n")
	put(t, filepath.Join(home, ".claude", "commands", "leak.md"), "use key sk-abcdefghijklmnopqrstuvwxyz123456 for calls")
	put(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"github":{"env":{"TOKEN":"ghp_x"}}}}`)

	b, err := Scan("claude", home, project)
	require.NoError(t, err)
	byName := map[string]Item{}
	for _, it := range b.Items {
		byName[it.Name] = it
	}
	require.Equal(t, "Review a diff", byName["claude-cmd-review"].Description)
	require.Equal(t, KindAgent, byName["claude-tester"].Kind)
	require.Contains(t, byName, "claude-deploy")
	require.Contains(t, byName, "claude-claude-user-instructions")
	require.NotContains(t, byName, "claude-cmd-leak")
	require.Len(t, b.Skipped, 1)
	require.Contains(t, b.Skipped[0], "credential")
	require.Len(t, b.Notes, 1)
	require.Contains(t, b.Notes[0], "github")
	require.NotContains(t, b.Notes[0], "ghp_x")
}

func TestScanOtherSources(t *testing.T) {
	t.Parallel()
	home, project := t.TempDir(), t.TempDir()
	put(t, filepath.Join(project, ".cursor", "rules", "style.mdc"), "---\ndescription: Style\nglobs: *.ts\n---\nUse tabs.\n")
	put(t, filepath.Join(home, ".gemini", "commands", "plan.toml"), "description = \"Plan work\"\nprompt = \"\"\"\nMake a plan.\n\"\"\"\n")
	put(t, filepath.Join(home, ".codex", "prompts", "fix.md"), "Fix the bug.\n")
	put(t, filepath.Join(project, ".agent", "workflows", "ship.md"), "Ship it.\n")

	cursor, err := Scan("cursor", home, project)
	require.NoError(t, err)
	require.Len(t, cursor.Items, 1)
	require.Equal(t, "Style", cursor.Items[0].Description)
	gemini, err := Scan("gemini", home, project)
	require.NoError(t, err)
	require.Len(t, gemini.Items, 1)
	require.Equal(t, "Plan work", gemini.Items[0].Description)
	require.Contains(t, gemini.Items[0].Body, "Make a plan.")
	codex, _ := Scan("codex", home, project)
	require.Len(t, codex.Items, 1)
	agy, _ := Scan("agy", home, project)
	require.Len(t, agy.Items, 1)

	_, err = Scan("vim", home, project)
	require.Error(t, err)
}

func TestImportInstallsADisabledReversiblePlugin(t *testing.T) {
	t.Parallel()
	home, project, root := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "plugins")
	put(t, filepath.Join(home, ".claude", "commands", "review.md"), "Look at the diff.\n")

	dry, err := Import("claude", home, project, root, true)
	require.NoError(t, err)
	require.Len(t, dry.Imported, 1)
	require.NoDirExists(t, root)

	res, err := Import("claude", home, project, root, false)
	require.NoError(t, err)
	require.Equal(t, "imported-claude", res.Plugin)
	manifest, err := plugin.Load(filepath.Join(root, "imported-claude"))
	require.NoError(t, err)
	require.False(t, plugin.IsEnabled(plugin.LoadState(root), manifest))
	skill, err := os.ReadFile(filepath.Join(root, "imported-claude", "skills", "claude-cmd-review", "SKILL.md"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(skill), "---\nname: claude-cmd-review\n"))

	require.NoError(t, plugin.Remove(root, "imported-claude"))
	require.NoDirExists(t, filepath.Join(root, "imported-claude"))

	_, err = Import("claude", t.TempDir(), t.TempDir(), root, false)
	require.Error(t, err)
}
