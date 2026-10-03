package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workflows"
	"github.com/stretchr/testify/require"
)

func TestSharedCommandsImportExportAndNoOverwrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "atlasrc"), []byte("exit 42\n"), 0o600))
	var output bytes.Buffer
	run := func(args ...string) error {
		output.Reset()
		command := newCommandsCommand()
		command.PersistentFlags().String("cwd", root, "test")
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(args)
		return command.ExecuteContext(t.Context())
	}
	require.NoError(t, run("export", "feature-delivery"))
	var recipe workflows.Recipe
	require.NoError(t, json.Unmarshal(output.Bytes(), &recipe))
	recipe.ID = "shared-fixture"
	data, err := json.Marshal(recipe)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "shared.json"), data, 0o600))
	require.NoError(t, run("import", "shared.json"))
	require.NoError(t, run("validate", recipe.ID))
	require.NoError(t, run("export", recipe.ID))
	var exported workflows.Recipe
	require.NoError(t, json.Unmarshal(output.Bytes(), &exported))
	require.Equal(t, recipe, exported)
	require.Error(t, run("import", "shared.json"))
	require.Error(t, run("import", "../outside.json"))
	installed, err := os.ReadFile(filepath.Join(root, ".atlas", "workflows", recipe.ID+".json"))
	require.NoError(t, err)
	require.JSONEq(t, string(data), string(installed))
}
