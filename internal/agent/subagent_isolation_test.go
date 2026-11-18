package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatchedFilesListsEachFileOnce(t *testing.T) {
	patch := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n" +
		"diff --git a/dir/b.go b/dir/b.go\nnew file mode 100644\n" +
		"diff --git a/a.go b/a.go\n"
	require.Equal(t, []string{"a.go", "dir/b.go"}, patchedFiles(patch))
	require.Empty(t, patchedFiles(""))
}

func TestIsolationIsRefusedWhenItCannotWork(t *testing.T) {
	c := &coordinator{}
	schema := verdictSchema(t)
	for name, tc := range map[string]struct {
		p    agentToolCallParams
		want string
	}{
		"unknown kind":     {agentToolCallParams{isolation: "container", agentName: "dev"}, `must be "worktree"`},
		"with workspace":   {agentToolCallParams{isolation: "worktree", agentName: "dev", workspaceID: "abc"}, "exclusive"},
		"with schema":      {agentToolCallParams{isolation: "worktree", agentName: "dev", resultSchema: schema}, "result_schema"},
		"unnamed agent":    {agentToolCallParams{isolation: "worktree"}, "named specialist"},
		"quality check":    {agentToolCallParams{isolation: "worktree", agentName: "dev", qualityOnly: true}, "named specialist"},
		"with session key": {agentToolCallParams{isolation: "worktree", agentName: "dev", sessionKey: "k"}, "named specialist"},
		"no durable store": {agentToolCallParams{isolation: "worktree", agentName: "dev"}, "durable storage"},
	} {
		id, refused := c.isolateRun(t.Context(), tc.p)
		require.Empty(t, id, name)
		require.NotNil(t, refused, name)
		require.True(t, refused.IsError, name)
		require.Contains(t, refused.Content, tc.want, name)
	}
}

func TestNoIsolationRequestedChangesNothing(t *testing.T) {
	id, refused := (&coordinator{}).isolateRun(t.Context(), agentToolCallParams{agentName: "dev"})
	require.Empty(t, id)
	require.Nil(t, refused)
}
