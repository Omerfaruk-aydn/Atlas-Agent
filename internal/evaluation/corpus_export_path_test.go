package evaluation

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeJoinRejectsEscapes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, path := range []string{"../outside", "nested/../../outside", "/absolute"} {
		_, err := safeJoin(root, path)
		require.Error(t, err, path)
	}
	path, err := safeJoin(root, "nested/file.go")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "nested", "file.go"), path)
}
