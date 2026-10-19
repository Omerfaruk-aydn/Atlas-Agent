package tools

import (
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

// newHistory returns a history service and the ID of a session to record
// in, since file versions belong to an existing session.
func newHistory(t *testing.T) (history.Service, string) {
	dataDir := t.TempDir()
	t.Cleanup(func() {
		require.NoError(t, db.Release(dataDir))
		db.ResetPool()
	})
	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)
	sess, err := session.NewService(db.New(conn), conn).Create(t.Context(), "history")
	require.NoError(t, err)
	return history.NewService(db.New(conn), conn), sess.ID
}

func resolvedContent(t *testing.T, files history.Service, sessionID string, messageIDs ...string) map[string]*string {
	resolved, err := files.ResolveAsOf(t.Context(), sessionID, messageIDs)
	require.NoError(t, err)
	out := map[string]*string{}
	for _, rf := range resolved {
		out[rf.Path] = rf.Content
	}
	return out
}

// A change recorded before and after, as the semantic edit plan does,
// resolves to the old content before the message and the new one after.
func TestFileHistoryBeforeAndAfter(t *testing.T) {
	files, s := newHistory(t)
	ctx := t.Context()
	root := t.TempDir()
	changed := historyPath(root, "pkg/a.go")
	created := historyPath(root, "pkg/b.go")

	require.NoError(t, recordFileBefore(ctx, files, s, changed, "old", true, "m1"))
	require.NoError(t, recordFileBefore(ctx, files, s, created, "", false, "m1"))
	require.NoError(t, recordFileAfter(ctx, files, s, changed, "new", "m1"))
	require.NoError(t, recordFileAfter(ctx, files, s, created, "made", "m1"))

	before := resolvedContent(t, files, s)
	require.Equal(t, "old", *before[changed])
	require.Nil(t, before[created], "a file the message created is deleted on rewind")

	after := resolvedContent(t, files, s, "m1")
	require.Equal(t, "new", *after[changed])
	require.Equal(t, "made", *after[created])
	require.Equal(t, filepath.Join(root, "pkg", "a.go"), changed)
}

func TestFileHistoryRecordsDriftWithoutAMessage(t *testing.T) {
	files, s := newHistory(t)
	ctx := t.Context()
	path := historyPath(t.TempDir(), "a.go")

	require.NoError(t, recordFileBefore(ctx, files, s, path, "old", true, "m1"))
	require.NoError(t, recordFileAfter(ctx, files, s, path, "new", "m1"))
	// The user edits the file outside the session before the next change.
	require.NoError(t, recordFileBefore(ctx, files, s, path, "by hand", true, "m2"))
	require.NoError(t, recordFileAfter(ctx, files, s, path, "newer", "m2"))

	require.Equal(t, "new", *resolvedContent(t, files, s, "m1")[path])
	require.Equal(t, "newer", *resolvedContent(t, files, s, "m1", "m2")[path])
	versions, err := files.ListBySession(ctx, s)
	require.NoError(t, err)
	require.Len(t, versions, 4, "baseline, m1, drift, m2")
}
