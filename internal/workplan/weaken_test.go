package workplan

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeakened(t *testing.T) {
	t.Parallel()
	before := &Plan{
		Verify: []Check{{Command: "make test"}, {Command: "make lint"}},
		Features: []Feature{
			{ID: "F1", Title: "a", Verify: "./f1.sh", Done: true},
			{ID: "F2", Title: "b", Verify: "./f2.sh", Acceptance: "shows b"},
			{ID: "F3", Title: "c", Done: true},
		},
	}
	after := &Plan{
		Verify: []Check{{Command: "make test"}, {Command: "make e2e"}},
		Features: []Feature{
			{ID: "F1", Title: "a renamed", Verify: "true", Done: true},
			{ID: "F2", Title: "b", Done: true},
		},
	}
	require.Equal(t, []string{
		"contract check `make lint` was removed or changed",
		"feature F1's check changed from `./f1.sh` to `true`",
		"feature F2 lost its check `./f2.sh`",
		"feature F3, already done, was removed from the plan",
	}, Weakened(before, after))
	require.Empty(t, Weakened(nil, after))
	require.Empty(t, Weakened(before, before))
}

func TestFlakyState(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := LoadState(root)
	require.True(t, s.AddFlaky("npm  test"))
	require.False(t, s.AddFlaky("npm test"), "whitespace does not make a new command")
	require.NoError(t, SaveState(root, s))
	require.True(t, LoadState(root).IsFlaky("npm test"))
	require.False(t, LoadState(root).IsFlaky("npm run build"))
}

func TestIsScratchDir(t *testing.T) {
	t.Parallel()
	require.True(t, IsScratchDir(".atlas/probes/F1"))
	require.True(t, IsScratchDir("/home/u/p/.atlas/alternatives/s-a/src"))
	require.False(t, IsScratchDir(".atlas/work.json"))
	require.False(t, IsScratchDir("src/alternatives/x"))
}
