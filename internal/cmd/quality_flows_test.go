package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func qualityCLI(t *testing.T, command *cobra.Command, root, data string, args ...string) string {
	t.Helper()
	parent := &cobra.Command{Use: "atlas-test"}
	parent.PersistentFlags().String("cwd", root, "")
	parent.PersistentFlags().String("data-dir", data, "")
	parent.AddCommand(command)
	var output bytes.Buffer
	parent.SetOut(&output)
	parent.SetErr(&output)
	parent.SetArgs(args)
	require.NoError(t, parent.ExecuteContext(t.Context()), output.String())
	return output.String()
}

func TestQualityCLIPrepareDoesNotExecuteCommands(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("Install using npm ci."), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "handover.json"), []byte(`{"version":1,"readme":"README.md","steps":[{"name":"install","argv":["npm","ci"],"readme_quote":"npm ci"}]}`), 0o600))
	result := qualityCLI(t, qualityAgentCommand("handover", "handover"), root, t.TempDir(), "handover", "handover.json", "--prepare-only")
	require.Contains(t, result, `"executed":false`)
	require.Contains(t, result, `"valid":true`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "journey.json"), []byte(`{"version":1,"url":"http://localhost:3000","cases":[{"name":"narrow","width":390,"height":844,"steps":[{"action":"assert","selector":"main","condition":"visible"}]}]}`), 0o600))
	result = qualityCLI(t, qualityAgentCommand("journey", "stress_journey"), root, t.TempDir(), "journey", "journey.json", "--prepare-only")
	require.Contains(t, result, "stress_journey")
	require.Contains(t, result, `"executed":false`)
}

func TestQualityCLIUndoPreviewThenApply(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	data := t.TempDir()
	g := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	g("init")
	g("config", "user.name", "Fixture")
	g("config", "user.email", "fixture@example.test")
	g("config", "core.autocrlf", "false")
	g("config", "core.hooksPath", filepath.Join(root, "no-hooks"))
	path := filepath.Join(root, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("base\n"), 0o600))
	g("add", ".")
	g("commit", "-m", "baseline")
	require.NoError(t, os.WriteFile(path, []byte("feature\n"), 0o600))
	g("add", ".")
	g("commit", "-m", "feature")
	preview := qualityCLI(t, newUndoCommand(), root, data, "undo", "feature", "--commit", "HEAD")
	var result struct {
		Plan struct {
			ID string `json:"id"`
		} `json:"plan"`
		Applied bool `json:"applied"`
	}
	require.NoError(t, json.Unmarshal([]byte(preview), &result))
	require.False(t, result.Applied)
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "feature\n", string(actual))
	qualityCLI(t, newUndoCommand(), root, data, "undo", "feature", "--apply", result.Plan.ID)
	actual, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "base\n", string(actual))
}

func TestQualityLabsCLIPreparationValidatesWithoutExecution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, fixture := range []struct{ command, tool, manifest string }{
		{"time-lab", "time_lab", `{"version":1,"url":"http://localhost:3000","clock":{"start":"2026-12-31T23:59:00Z"},"cases":[{"name":"expiry","width":1280,"height":800,"steps":[{"action":"advance_time","advance_ms":120000},{"action":"assert","selector":"#status","condition":"text","expected":"expired"}]}]}`},
		{"dataset-forge", "dataset_forge", `{"version":1,"seed":42,"output":"fixture.json","entities":[{"name":"users","count":2,"fields":[{"name":"id","kind":"id"}]}]}`},
		{"assumption-check", "assumption_check", `{"version":1,"checks":[{"name":"unique","file":"fixture.json","pointer":"/users","rule":"unique","field":"/id"}]}`},
		{"data-boundary", "data_boundary", `{"version":1,"action":"prepare","labels":["customer"]}`},
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "manifest.json"), []byte(fixture.manifest), 0o600))
		result := qualityCLI(t, qualityAgentCommand(fixture.command, fixture.tool), root, t.TempDir(), fixture.command, "manifest.json", "--prepare-only")
		require.Contains(t, result, `"executed":false`)
		require.Contains(t, result, fixture.tool)
	}
	_, err := os.Stat(filepath.Join(root, "fixture.json"))
	require.True(t, os.IsNotExist(err))
}
