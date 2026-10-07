package skills

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinAddressesRemainCompatibleWithoutRewritingDiskPaths(t *testing.T) {
	t.Parallel()
	require.True(t, IsBuiltinPath("atlas://skills/jq/SKILL.md"))
	require.True(t, IsBuiltinPath("crush://skills/jq/SKILL.md"))
	require.Equal(t, "atlas://skills/jq/SKILL.md", CanonicalBuiltinPath("crush://skills/jq/SKILL.md"))
	for _, path := range []string{"C:/project/crush/file.md", "crush://other/file.md", "atlas://other/file.md"} {
		require.False(t, IsBuiltinPath(path))
		require.Equal(t, path, CanonicalBuiltinPath(path))
	}
}
