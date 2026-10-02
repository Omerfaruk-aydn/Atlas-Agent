package skills

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesignSearch(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"açıklama", "light", "color"}, designTokens("açıklama açık renk"))
	for domain, count := range map[string]int{"style": 67, "color": 96, "typography": 57, "ux": 99} {
		rows, err := designRows(domain, "")
		require.NoError(t, err)
		require.Len(t, rows, count+1)
	}
	a, err := SearchDesign("minimal clean", "style", "", 2)
	require.NoError(t, err)
	require.Len(t, a, 2)
	b, err := SearchDesign("minimal clean", "style", "", 2)
	require.NoError(t, err)
	require.Equal(t, a, b)
	empty, err := SearchDesign("zzzzunfindablezzz", "ux", "", 3)
	require.NoError(t, err)
	require.Empty(t, empty)
	for _, domain := range []string{"unknown", "../../atlas-config/SKILL.md", `C:\\secret`} {
		_, err := SearchDesign("test", domain, "", 2)
		require.Error(t, err)
	}
	_, err = SearchDesign("test", "stack", "../../secret", 2)
	require.Error(t, err)
	_, err = SearchDesign("test", "ux", "", 21)
	require.Error(t, err)
	_, err = SearchDesign("", "ux", "", 2)
	require.Error(t, err)
	_, err = SearchDesign(strings.Repeat("a", 4097), "ux", "", 2)
	require.Error(t, err)
	for _, stack := range designStacks {
		rows, err := designRows("stack", stack)
		require.NoError(t, err, stack)
		require.Greater(t, len(rows), 1, stack)
	}
	for domain := range designDomains {
		rows, err := designRows(domain, "")
		require.NoError(t, err, domain)
		require.Greater(t, len(rows), 1, domain)
	}
	malformed, err := SearchDesign("canonical", "stack", "astro", 1)
	require.NoError(t, err)
	require.Len(t, malformed, 1)
	require.NotEmpty(t, malformed[0].Fields["Data warning"])
	require.NotEmpty(t, malformed[0].Fields["Unstructured reference"])
	turkish, err := SearchDesign("erişilebilirlik", "ux", "", 2)
	require.NoError(t, err)
	require.NotEmpty(t, turkish)
}

func TestDesignBuiltins(t *testing.T) {
	t.Parallel()
	all := DiscoverBuiltin()
	for _, name := range []string{"ui-ux-pro-max", "apple-design", "tui-design"} {
		var found *Skill
		for _, s := range all {
			if s.Name == name {
				found = s
			}
		}
		require.NotNil(t, found, name)
		require.NoError(t, found.Validate())
		require.True(t, found.Builtin)
		_, err := BuiltinFS().ReadFile("builtin/" + name + "/SKILL.md")
		require.NoError(t, err)
	}
	entries, err := fs.Glob(BuiltinFS(), "builtin/ui-ux-pro-max/data/*.csv")
	require.NoError(t, err)
	require.Len(t, entries, 11)
	entries, err = fs.Glob(BuiltinFS(), "builtin/ui-ux-pro-max/data/stacks/*.csv")
	require.NoError(t, err)
	require.Len(t, entries, 13)
}
