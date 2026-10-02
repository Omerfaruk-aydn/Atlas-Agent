package shellconfig

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecipesWorkflowPaths(t *testing.T) {
	t.Parallel()
	data, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte("option workflow-path ./one\noption reset workflow-path\noption workflow-path ./two\n"))
	require.NoError(t, err)
	var config struct {
		Options struct {
			Paths []string `json:"workflow_paths"`
		} `json:"options"`
	}
	require.NoError(t, json.Unmarshal(data, &config))
	require.Equal(t, []string{"./two"}, config.Options.Paths)
}
