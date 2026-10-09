package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clibridge"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestCLIBridgeCommandFixture is the real program the command tests run.
func TestCLIBridgeCommandFixture(t *testing.T) {
	i := slices.Index(os.Args, "cli-bridge-command-fixture")
	if i < 0 || i+1 >= len(os.Args) {
		t.Skip("helper process for CLI Bridge command tests")
	}
	if err := os.WriteFile(os.Args[i+1], []byte("ran"), 0o600); err != nil {
		os.Exit(9)
	}
	os.Exit(0)
}

func bridgeCLI(t *testing.T, root, data string, args ...string) (string, error) {
	t.Helper()
	parent := &cobra.Command{Use: "atlas-test", SilenceUsage: true, SilenceErrors: true}
	parent.PersistentFlags().String("cwd", root, "")
	parent.PersistentFlags().String("data-dir", data, "")
	parent.AddCommand(newCLIBridgeCommand())
	var stdout, stderr bytes.Buffer
	parent.SetOut(&stdout)
	parent.SetErr(&stderr)
	parent.SetArgs(append([]string{"cli-bridge"}, args...))
	err := parent.ExecuteContext(t.Context())
	return stdout.String(), err
}

func writeBridgeFixtureProfile(t *testing.T, root string) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	profile, err := json.Marshal(map[string]any{
		"kind": clibridge.ProfileKind, "version": 1, "id": "marker", "description": "Create a marker file.",
		"executable": map[string]any{"path": exe},
		"operations": []any{map[string]any{
			"id": "touch", "description": "Create the marker.",
			"args":    []any{"-test.run=^TestCLIBridgeCommandFixture$", "--", "cli-bridge-command-fixture", "{output}"},
			"params":  []any{map[string]any{"name": "output", "type": "output_file", "required": true}},
			"outputs": []any{map[string]any{"param": "output", "min_bytes": 1}},
		}},
	})
	require.NoError(t, err)
	path := filepath.Join(root, "marker-profile.json")
	require.NoError(t, os.WriteFile(path, profile, 0o600))
	return path
}

func TestCLIBridgeCommandsRunNothingUntilExplicitRun(t *testing.T) {
	// Not parallel: isolates global configuration and the user profile dir.
	for _, name := range []string{"GLOBAL_CONFIG", "GLOBAL_DATA", "CACHE_DIR", "CLI_BRIDGE_DIR"} {
		t.Setenv("ATLAS_AGENT_"+name, t.TempDir())
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	data := filepath.Join(root, ".atlas")
	writeBridgeFixtureProfile(t, root)
	marker := filepath.Join(root, "marker.txt")

	out, err := bridgeCLI(t, root, data, "validate", "marker-profile.json", "--operation", "touch", "--arg", "output=marker.txt")
	require.NoError(t, err)
	require.Contains(t, out, `"executed": false`)
	require.Contains(t, out, `"executable_status": "available"`)

	out, err = bridgeCLI(t, root, data, "import", "marker-profile.json")
	require.NoError(t, err)
	require.Contains(t, out, `"executed": false`)
	_, err = bridgeCLI(t, root, data, "import", "marker-profile.json", "--scope", "user")
	require.ErrorContains(t, err, "already exists in project scope")

	out, err = bridgeCLI(t, root, data, "list")
	require.NoError(t, err)
	require.Contains(t, out, `"id": "marker"`)
	out, err = bridgeCLI(t, root, data, "inspect", "project:marker")
	require.NoError(t, err)
	require.Contains(t, out, `"status": "available"`)
	out, err = bridgeCLI(t, root, data, "preview", "marker", "touch", "output=marker.txt")
	require.NoError(t, err)
	require.Contains(t, out, `"executed": false`)
	require.NoFileExists(t, marker, "validate, import, list, inspect and preview ran nothing")

	_, err = bridgeCLI(t, root, data, "run", "marker", "touch", "output=marker.txt")
	require.ErrorContains(t, err, "--allow-execution")
	require.NoFileExists(t, marker)

	out, err = bridgeCLI(t, root, data, "run", "marker", "touch", "output=marker.txt", "--allow-execution", "--session-id", "cli-session")
	require.NoError(t, err, out)
	require.FileExists(t, marker)
	var report clibridge.RunReport
	require.NoError(t, json.Unmarshal([]byte(out), &report), out)
	require.True(t, report.Passed)
	require.Equal(t, 0, *report.ExitCode)

	out, err = bridgeCLI(t, root, data, "result", report.RunID, "--session-id", "cli-session")
	require.NoError(t, err)
	require.Contains(t, out, report.RunID)
	require.Contains(t, out, `"outcome": "passed"`)

	_, err = bridgeCLI(t, root, data, "run", "marker", "touch", "output=marker.txt", "--allow-execution", "--session-id", "cli-session")
	require.Error(t, err, "an existing output is refused before running again")
}

func TestCLIBridgeDraftFromSavedHelpAndExamples(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "help.txt"), []byte("Usage: tool\n  -o, --output FILE  write here\n  --fast\n"), 0o600))
	out, err := bridgeCLI(t, root, filepath.Join(root, ".atlas"), "draft", "atlas-cli-bridge-missing-tool", "--id", "tool", "--help-file", "help.txt")
	require.NoError(t, err)
	require.Contains(t, out, `"active": false`)
	draft := clibridge.DraftPath(root, "tool")
	require.FileExists(t, draft)
	_, err = bridgeCLI(t, root, filepath.Join(root, ".atlas"), "import", draft)
	require.ErrorContains(t, err, "help-derived draft")

	_, err = bridgeCLI(t, root, filepath.Join(root, ".atlas"), "draft", "atlas-cli-bridge-missing-tool", "--id", "tool2")
	require.ErrorContains(t, err, "--allow-execution")

	out, err = bridgeCLI(t, root, filepath.Join(root, ".atlas"), "examples")
	require.NoError(t, err)
	require.Contains(t, out, "ffmpeg-video")
	out, err = bridgeCLI(t, root, filepath.Join(root, ".atlas"), "examples", "rg-search")
	require.NoError(t, err)
	_, err = clibridge.ParseProfile([]byte(out))
	require.NoError(t, err)
}
