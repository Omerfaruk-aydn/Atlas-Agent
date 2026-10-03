package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesignReferencesMatchCanonicalManifest(t *testing.T) {
	t.Parallel()
	data, err := BuiltinFS().ReadFile("builtin/ui-ux-pro-max/REFERENCE_MANIFEST.json")
	require.NoError(t, err)
	var files []struct {
		Path   string `json:"path"`
		Source string `json:"source_sha256"`
		LF     string `json:"lf_sha256"`
	}
	require.NoError(t, json.Unmarshal(data, &files))
	require.Len(t, files, 24)
	seen := map[string]bool{}
	for _, file := range files {
		require.False(t, seen[file.Path])
		seen[file.Path] = true
		source, err := hex.DecodeString(file.Source)
		require.NoError(t, err)
		require.Len(t, source, 32)
		content, err := BuiltinFS().ReadFile("builtin/ui-ux-pro-max/data/" + file.Path)
		require.NoError(t, err)
		hash := sha256.Sum256([]byte(strings.ReplaceAll(string(content), "\r\n", "\n")))
		require.Equal(t, file.LF, hex.EncodeToString(hash[:]), file.Path)
	}
}
