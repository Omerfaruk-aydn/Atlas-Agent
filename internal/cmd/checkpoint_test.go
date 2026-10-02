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
	"github.com/stretchr/testify/require"
)

func TestCheckpointCLIPlansWithoutConfigurationExecutionOrReplay(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package fixture\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "atlasrc"), []byte("printf unexpected > marker.txt\n"), 0o644))
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(dir) })
	service := session.NewService(db.New(conn), conn)
	sess, err := service.Create(t.Context(), "resume")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "next", Content: "Next slice", Status: session.TodoStatusPending, OwnedPaths: []string{"."}}}
	_, err = service.Save(t.Context(), sess)
	require.NoError(t, err)
	var output bytes.Buffer
	run := func(args ...string) {
		output.Reset()
		command := newWorkflowCommand()
		command.PersistentFlags().String("cwd", root, "")
		command.PersistentFlags().String("data-dir", dir, "")
		command.SetOut(&output)
		command.SetArgs(args)
		require.NoError(t, command.ExecuteContext(t.Context()))
	}
	run("checkpoint", sess.ID)
	var cp engineering.Checkpoint
	require.NoError(t, json.Unmarshal(output.Bytes(), &cp))
	before, err := engineering.NewStore(dir).Read(t.Context(), sess.ID)
	require.NoError(t, err)
	run("resume-plan", sess.ID, cp.ID)
	var plan engineering.ResumePlan
	require.NoError(t, json.Unmarshal(output.Bytes(), &plan))
	require.Equal(t, []string{"next"}, plan.ReadyTasks)
	after, err := engineering.NewStore(dir).Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = os.Stat(filepath.Join(root, "marker.txt"))
	require.True(t, os.IsNotExist(err))
}
