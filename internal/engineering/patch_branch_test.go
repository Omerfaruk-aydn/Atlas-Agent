package engineering

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatchBranchesIsolateProposalsAndDetectLiveConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := NewStore(t.TempDir())
	path := filepath.Join(root, "api.go")
	require.NoError(t, os.WriteFile(path, []byte("original\n"), 0o644))
	a, err := store.ForkPatchBranch(t.Context(), "s", root, "algorithm-a", "task", []string{"api.go"})
	require.NoError(t, err)
	b, err := store.ForkPatchBranch(t.Context(), "s", root, "algorithm-b", "task", []string{"api.go"})
	require.NoError(t, err)
	proposed := "alternative\n"
	_, err = store.ProposeBranch(t.Context(), "s", a.ID, []BranchFile{{Path: "api.go", Proposed: &proposed}}, []string{"Only A assumes sorting"})
	require.NoError(t, err)
	st, err := store.Read(t.Context(), "s")
	require.NoError(t, err)
	require.Equal(t, b.ID, st.Branches[1].ID)
	require.Nil(t, st.Branches[1].Files[0].Proposed)
	require.Empty(t, st.Branches[1].Notes)
	current, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "original\n", string(current))
	require.NoError(t, os.WriteFile(path, []byte("user edit\n"), 0o644))
	_, err = store.BeginBranchMerge(t.Context(), "s", root, a.ID)
	require.ErrorContains(t, err, "conflict")
	current, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "user edit\n", string(current))
	require.NoError(t, os.WriteFile(path, []byte("original\n"), 0o644))
	_, err = store.BeginBranchMerge(t.Context(), "s", root, a.ID)
	require.NoError(t, err)
	_, err = store.BeginBranchMerge(t.Context(), "s", root, a.ID)
	require.ErrorContains(t, err, "replay")
}

func TestPatchBranchRejectsEscapedSources(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	_, err := store.ForkPatchBranch(t.Context(), "s", t.TempDir(), "branch", "", []string{"../outside"})
	require.Error(t, err)
}
