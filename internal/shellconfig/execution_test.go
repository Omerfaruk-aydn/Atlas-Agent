package shellconfig

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecutionOptionsInAtlasrc(t *testing.T) {
	t.Parallel()
	script := `option execution mode container-required
option execution runtime-path "C:/runtime/docker.exe"
option execution network none
option execution cpus 2
option execution memory-bytes 2147483648
option execution max-processes 128
option execution timeout-ms 600000
option execution read-only true
option execution environment-key LANG`
	data, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte(script))
	require.NoError(t, err)
	var result struct {
		Options struct {
			Execution struct {
				Mode            string   `json:"mode"`
				CPUs            float64  `json:"cpus"`
				ReadOnly        bool     `json:"read_only"`
				EnvironmentKeys []string `json:"environment_keys"`
			} `json:"execution"`
		} `json:"options"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.Equal(t, "container-required", result.Options.Execution.Mode)
	require.Equal(t, float64(2), result.Options.Execution.CPUs)
	require.True(t, result.Options.Execution.ReadOnly)
	require.Equal(t, []string{"LANG"}, result.Options.Execution.EnvironmentKeys)
}
