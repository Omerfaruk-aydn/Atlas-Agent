package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpecialistSkillsAndTemplateResourcesAreEmbedded(t *testing.T) {
	t.Parallel()
	all, states := DiscoverBuiltinWithStates()
	for _, state := range states {
		require.NoError(t, state.Err, state.Path)
	}
	names := map[string]bool{}
	for _, skill := range all {
		names[skill.Name] = true
	}
	for _, name := range []string{"design-system", "visual-quality", "motion-design", "desktop-automation", "browser-automation", "office-documents", "office-presentations", "data-analysis", "artifact-templates", "mcp-integration", "team-operations", "security-evidence", "patch-review", "technical-diagrams", "research-evidence", "animated-mascots"} {
		require.True(t, names[name], name)
	}
	catalog, err := BuiltinFS().ReadFile("builtin/artifact-templates/TEMPLATES.md")
	require.NoError(t, err)
	require.Equal(t, 20, strings.Count(string(catalog), "- Artifact type:"))
	require.Contains(t, string(catalog), "Three-Statement Forecast")
	require.Contains(t, string(catalog), "source fidelity")
}
