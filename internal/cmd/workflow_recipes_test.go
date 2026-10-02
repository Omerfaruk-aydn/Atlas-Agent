package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workflows"
	"github.com/stretchr/testify/require"
)

func TestRecipesCLIPlanOnlyDoesNotLoadConfigOrDatabase(t *testing.T) {
	t.Parallel()
	root, dir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "atlasrc"), []byte("touch should-not-run\nexit 42\n"), 0o600))
	database := filepath.Join(dir, "atlas.db")
	require.NoError(t, os.WriteFile(database, []byte("not a database"), 0o600))
	var output bytes.Buffer
	run := func(args ...string) error {
		output.Reset()
		command := newWorkflowCommand()
		command.PersistentFlags().String("cwd", root, "test")
		command.PersistentFlags().String("data-dir", dir, "test")
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(args)
		return command.Execute()
	}
	require.NoError(t, run("recipes"))
	var recipes []workflows.Recipe
	require.NoError(t, json.Unmarshal(output.Bytes(), &recipes))
	require.Len(t, recipes, 3)
	require.NoError(t, run("validate", "release-check"))
	require.NoError(t, run("run", "feature-delivery", "--plan-only", "--param", `feature="$(touch escaped) ; echo literal"`))
	var compiled workflows.Compiled
	require.NoError(t, json.Unmarshal(output.Bytes(), &compiled))
	require.Equal(t, root, compiled.Plan.Root)
	require.Contains(t, compiled.Tasks[0].Content, "$(touch escaped) ; echo literal")
	for _, name := range []string{"should-not-run", "escaped"} {
		_, err := os.Stat(filepath.Join(root, name))
		require.True(t, os.IsNotExist(err))
	}
	data, err := os.ReadFile(database)
	require.NoError(t, err)
	require.Equal(t, "not a database", string(data))
	_, err = os.Stat(filepath.Join(dir, "engineering"))
	require.True(t, os.IsNotExist(err), "plan-only does not read or create state")
	require.Error(t, run("run", "feature-delivery", "--plan-only", "--param", `feature=null`))
	require.Error(t, run("run", "feature-delivery", "--plan-only", "--param", `feature="x"`, "--param", `feature="y"`))
}

func TestRecipesCLIAdmissionAndCheckpoint(t *testing.T) {
	t.Parallel()
	root, dir := t.TempDir(), t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	sess, err := session.NewService(db.New(conn), conn).Create(t.Context(), "recipe")
	require.NoError(t, err)
	require.NoError(t, db.Release(dir))
	var output bytes.Buffer
	run := func(args ...string) error {
		output.Reset()
		command := newWorkflowCommand()
		command.PersistentFlags().String("cwd", root, "test")
		command.PersistentFlags().String("data-dir", dir, "test")
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(args)
		return command.Execute()
	}
	require.NoError(t, run("run", "release-check", "--session-id", sess.ID, "--param", `version="0.14.2"`))
	var admitted workflows.RecipeRun
	require.NoError(t, json.Unmarshal(output.Bytes(), &admitted))
	require.Equal(t, "active", admitted.Status)
	require.NoError(t, run("checkpoint", sess.ID))
	var cp engineering.Checkpoint
	require.NoError(t, json.Unmarshal(output.Bytes(), &cp))
	require.Equal(t, admitted.RecipeHash, cp.RecipeHash)
	require.NoError(t, run("resume-plan", sess.ID, cp.ID))
	require.Error(t, run("run", "feature-delivery", "--session-id", sess.ID, "--param", `feature="overwrite"`))
}
