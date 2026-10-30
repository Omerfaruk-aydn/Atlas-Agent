package shellconfig

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHarnessTraceAndExplicitAblationOptions(t *testing.T) {
	t.Parallel()
	data, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte("option harness-trace false\noption harness-disable-gate evaluator"))
	require.NoError(t, err)
	var cfg struct {
		Options struct {
			Harness struct {
				Trace         *bool    `json:"trace"`
				DisabledGates []string `json:"disabled_gates"`
			} `json:"harness"`
		} `json:"options"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg))
	require.NotNil(t, cfg.Options.Harness.Trace)
	require.False(t, *cfg.Options.Harness.Trace)
	require.Contains(t, cfg.Options.Harness.DisabledGates, "evaluator")
	for _, script := range []string{"option harness-trace banana", "option harness-disable-gate imaginary", "option harness-mutation-max -1"} {
		_, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte(script))
		require.Error(t, err)
	}
}

func TestHarnessContextOptions(t *testing.T) {
	t.Parallel()
	script := "option harness-attachment-reference-bytes 32768\n" +
		"option harness-idle-compact-minutes 10\n" +
		"option harness-idle-compact-tokens 64000"
	data, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte(script))
	require.NoError(t, err)
	var cfg struct {
		Options struct {
			Harness struct {
				Attachment int `json:"attachment_reference_bytes"`
				Minutes    int `json:"idle_compact_minutes"`
				Tokens     int `json:"idle_compact_tokens"`
			} `json:"harness"`
		} `json:"options"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg))
	require.Equal(t, 32768, cfg.Options.Harness.Attachment)
	require.Equal(t, 10, cfg.Options.Harness.Minutes)
	require.Equal(t, 64000, cfg.Options.Harness.Tokens)
	_, err = LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte("option harness-attachment-reference-bytes -2"))
	require.Error(t, err)
}
