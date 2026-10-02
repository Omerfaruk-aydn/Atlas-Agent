package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestWorkflowSnapshotCLIUsesSharedReadModelWithoutExecutableConfig(t *testing.T) {
	t.Setenv("ATLAS_AGENT_CLIENT_SERVER", "false")
	root, dataDir := t.TempDir(), t.TempDir()
	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)
	sessions := session.NewService(db.New(conn), conn)
	sess, err := sessions.Create(t.Context(), "snapshot")
	require.NoError(t, err)
	defer db.Release(dataDir)
	require.NoError(t, os.WriteFile(filepath.Join(root, "atlasrc"), []byte("exit 99\n"), 0o600))
	store := engineering.NewStore(dataDir)
	shared, err := agent.ReadWorkflowSnapshot(t.Context(), store, sessions, root, sess.ID, false)
	require.NoError(t, err)
	command := newWorkflowCommand()
	command.PersistentFlags().String("cwd", root, "fixture")
	command.PersistentFlags().String("data-dir", dataDir, "fixture")
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{"snapshot", sess.ID})
	require.NoError(t, command.Execute())
	var cli engineering.WorkflowSnapshot
	require.NoError(t, json.Unmarshal(out.Bytes(), &cli))
	require.Equal(t, shared, cli)
}
