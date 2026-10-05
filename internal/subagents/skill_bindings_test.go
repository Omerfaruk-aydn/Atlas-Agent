package subagents

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestAllSpecialistsHaveResolvableIndependentSkillBindings(t *testing.T) {
	t.Parallel()
	available := make(map[string]bool)
	for _, skill := range skills.DiscoverBuiltin() {
		available[skill.Name] = true
	}
	for _, role := range Builtin() {
		require.NotEmpty(t, role.PreferredSkills, role.Name)
		for _, name := range role.PreferredSkills {
			require.True(t, available[name], "%s references unavailable builtin %s", role.Name, name)
		}
		content, err := Render(role)
		require.NoError(t, err)
		parsed, err := ParseContent(content)
		require.NoError(t, err)
		require.Equal(t, role.PreferredSkills, parsed.PreferredSkills)
		require.Equal(t, role.InheritModel, parsed.InheritModel)
	}
	first := Builtin()
	first[0].PreferredSkills[0] = "corrupted"
	require.NotEqual(t, "corrupted", Builtin()[0].PreferredSkills[0])
}

func TestSkillBindingsRejectUnsafeDuplicateAndUnboundedNames(t *testing.T) {
	t.Parallel()
	for _, names := range [][]string{{"../outside"}, {"design-system", "design-system"}, {""}, {"a", "b", "c", "d", "e", "f", "g", "h", "i"}} {
		s := Subagent{Name: "fixture", Description: "Fixture", PreferredSkills: names}
		require.Error(t, s.Validate())
	}
}

func TestSpecialistRoutingRecognizesDomainsAndEnforcesActualTools(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		prompt, taskType, name string
	}{
		{"Figma tasarla", "product-design", "product-designer"},
		{"görsel kontrol", "visual-quality", "visual-qa"},
		{"animasyon oluştur", "motion", "motion-designer"},
		{"masaüstü uygulamasını kullan", "desktop", "desktop-operator"},
		{"tarayıcıda form doldur", "browser", "browser-operator"},
		{"PDF belge oluştur", "document", "documents"},
		{"sunum hazırla", "presentation", "presentations"},
		{"CSV analiz et", "data-analysis", "data-analyst"},
		{"şablon oluştur", "template", "template-builder"},
		{"MCP entegrasyon oluştur", "integration", "integration-engineer"},
		{"takvim toplantı düzenle", "operations", "operations"},
	} {
		got, ok := Route(Builtin(), RouteRequest{Prompt: tc.prompt, TaskType: tc.taskType}, nil)
		require.True(t, ok, tc.name)
		require.Equal(t, tc.name, got.Name)
		got, ok = Route(Builtin(), RouteRequest{Prompt: tc.prompt}, nil)
		require.True(t, ok, tc.name)
		require.Equal(t, tc.name, got.Name)
	}
	_, ok := Route(Builtin(), RouteRequest{TaskType: "desktop"}, []string{"view", "grep"})
	require.False(t, ok)
	_, ok = Route(Builtin(), RouteRequest{TaskType: "browser"}, []string{"computer"})
	require.False(t, ok)
}
