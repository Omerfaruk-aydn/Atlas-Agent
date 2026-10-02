package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/projects"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestSessionOwnerFindsShortHashOutsideProjectWithoutCreatingDatabase(t *testing.T) {
	t.Parallel()
	root, home := t.TempDir(), t.TempDir()
	data := filepath.Join(root, ".atlas")
	conn, err := db.Connect(t.Context(), data)
	require.NoError(t, err)
	service := session.NewService(db.New(conn), conn)
	sess, err := service.Create(t.Context(), "Existing conversation")
	require.NoError(t, err)
	db.Release(data)
	registered := []projects.Project{{Path: root, DataDir: data}}
	for _, id := range []string{sess.ID, session.HashID(sess.ID)[:7]} {
		owner, err := findSessionOwner(t.Context(), id, home, "", registered, false)
		require.NoError(t, err)
		require.Equal(t, root, owner.Root)
		require.Equal(t, sess.ID, owner.SessionID)
	}
	_, err = os.Stat(filepath.Join(home, ".atlas"))
	require.True(t, os.IsNotExist(err))
	_, err = findSessionOwner(t.Context(), session.HashID(sess.ID)[:7], home, "", registered, true)
	require.ErrorContains(t, err, "not found")
}

func TestSessionOwnerAmbiguityAndCorruptDatabaseFailBeforeWorkspaceSetup(t *testing.T) {
	t.Parallel()
	var registered []projects.Project
	for range 2 {
		root := t.TempDir()
		data := filepath.Join(root, ".atlas")
		conn, err := db.Connect(t.Context(), data)
		require.NoError(t, err)
		_, err = session.NewService(db.New(conn), conn).Create(t.Context(), "Conversation")
		require.NoError(t, err)
		db.Release(data)
		registered = append(registered, projects.Project{Path: root, DataDir: data})
	}
	_, err := findSessionOwner(t.Context(), "not-a-session", t.TempDir(), "", registered, false)
	require.ErrorContains(t, err, "not found")
	// The same database registered twice is one owner, not two matches.
	conn, err := db.ConnectReadOnly(t.Context(), filepath.Join(registered[0].DataDir, "atlas.db"))
	require.NoError(t, err)
	list, err := session.NewService(db.New(conn), conn).List(t.Context())
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	_, err = findSessionOwner(t.Context(), list[0].ID, t.TempDir(), "", append(registered, registered[0]), false)
	require.NoError(t, err)
	duplicateRoot := t.TempDir()
	duplicateData := filepath.Join(duplicateRoot, ".atlas")
	require.NoError(t, os.MkdirAll(duplicateData, 0o700))
	data, err := os.ReadFile(filepath.Join(registered[0].DataDir, "atlas.db"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(duplicateData, "atlas.db"), data, 0o600))
	_, err = findSessionOwner(t.Context(), list[0].ID, t.TempDir(), "", append(registered, projects.Project{Path: duplicateRoot, DataDir: duplicateData}), false)
	require.ErrorContains(t, err, "ambiguous")
	bad := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bad, "atlas.db"), []byte("corrupt"), 0o600))
	_, err = findSessionOwner(t.Context(), list[0].ID, t.TempDir(), "", append(registered, projects.Project{Path: bad, DataDir: bad}), false)
	require.ErrorContains(t, err, "could not be inspected")
}

func TestSessionWorkspaceSelectionUpdatesFlagsBeforeOpeningWorkspace(t *testing.T) {
	global := t.TempDir()
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", filepath.Join(global, "Atlas-Agent"))
	t.Setenv("XDG_DATA_HOME", global)
	root, home := t.TempDir(), t.TempDir()
	data := filepath.Join(root, ".atlas")
	conn, err := db.Connect(t.Context(), data)
	require.NoError(t, err)
	sess, err := session.NewService(db.New(conn), conn).Create(t.Context(), "Existing")
	require.NoError(t, err)
	db.Release(data)
	require.NoError(t, projects.Register(root, data))
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.Flags().String("cwd", home, "")
	cmd.Flags().String("data-dir", "", "")
	cmd.Flags().String("session", session.HashID(sess.ID)[:7], "")
	require.NoError(t, selectSessionWorkspace(cmd))
	selected, _ := cmd.Flags().GetString("cwd")
	require.Equal(t, root, selected)
	selected, _ = cmd.Flags().GetString("session")
	require.Equal(t, sess.ID, selected)
	_, err = os.Stat(filepath.Join(home, ".atlas"))
	require.True(t, os.IsNotExist(err))
}
