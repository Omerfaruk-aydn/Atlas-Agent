package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// atlasrc and atlas.json must agree, and an explicit false in a later layer
// must survive the default.
func TestDeferToolSchemasDefaultsAndExplicitChoices(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		global string
		want   bool
	}{
		{name: "unset", want: true},
		{name: "json false", files: map[string]string{"atlas.json": `{"options":{"defer_tool_schemas":false}}`}, want: false},
		{name: "json true", files: map[string]string{"atlas.json": `{"options":{"defer_tool_schemas":true}}`}, want: true},
		{name: "atlasrc false", files: map[string]string{"atlasrc": "option defer-tool-schemas false\n"}, want: false},
		{name: "atlasrc true", files: map[string]string{"atlasrc": "option defer-tool-schemas true\n"}, want: true},
		{name: "workspace false beats global true", global: `{"options":{"defer_tool_schemas":true}}`, files: map[string]string{"atlas.json": `{"options":{"defer_tool_schemas":false}}`}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			globalDir := t.TempDir()
			t.Setenv("ATLAS_AGENT_GLOBAL_CONFIG", globalDir)
			t.Setenv("ATLAS_AGENT_GLOBAL_DATA", t.TempDir())
			t.Setenv("ATLAS_AGENT_CACHE_DIR", t.TempDir())
			if tc.global != "" {
				require.NoError(t, os.WriteFile(filepath.Join(globalDir, "atlas.json"), []byte(tc.global), 0o644))
			}
			dir := t.TempDir()
			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
			}
			store, err := Init(dir, "", false)
			require.NoError(t, err)
			require.Equal(t, tc.want, store.Config().Options.DeferToolSchemasEnabled())
		})
	}
}
