package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesignCraftSkillsAreEmbedded(t *testing.T) {
	t.Parallel()
	all, states := DiscoverBuiltinWithStates()
	for _, state := range states {
		require.NoError(t, state.Err, state.Path)
	}
	byName := map[string]*Skill{}
	for _, skill := range all {
		byName[skill.Name] = skill
	}
	for _, name := range []string{"design-brief", "design-critique", "anti-slop", "type-and-color", "design-states", "mobile-app-design", "web-landing-design", "dashboard-design", "design-taste", "design-script", "design-kit", "design-patterns", "design-director"} {
		skill, ok := byName[name]
		require.True(t, ok, name)
		require.NoError(t, skill.Validate(), name)
		require.Equal(t, BuiltinPrefix+name+"/SKILL.md", skill.SkillFilePath)
		require.Greater(t, len(strings.TrimSpace(skill.Instructions)), 500, name)
	}
}
