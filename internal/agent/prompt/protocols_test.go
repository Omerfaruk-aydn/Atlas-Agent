package prompt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskProtocolsSelectIntentWithoutSubstringMatches(t *testing.T) {
	t.Parallel()
	require.Empty(t, SelectProtocols("Fix one spelling in README", ""))
	require.Empty(t, SelectProtocols("capital quick build", ""))
	ordered, err := CanonicalProtocols([]string{"ui", "architecture", "ui"})
	require.NoError(t, err)
	require.Equal(t, []string{"architecture", "ui"}, ordered)
	ui := SelectProtocols("TUI arayüzü ve klavye akışını geliştir", "frontend")
	require.Equal(t, []string{"ui"}, ui)
	require.Contains(t, SelectProtocols("sqlite migration for backend API", ""), "migration")
	require.Contains(t, SelectProtocols("", "architect"), "architecture")
	rendered, err := RenderProtocols(ui)
	require.NoError(t, err)
	require.Contains(t, rendered, "tui-design")
	require.Contains(t, rendered, "apple-design")
	require.NotContains(t, rendered, `name="migration"`)
	_, err = RenderProtocols([]string{"../../untrusted"})
	require.Error(t, err)
}

func TestSpecialistProtocolsFollowRolesAndRemainSelective(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ role, protocol string }{
		{"desktop-operator", "automation"},
		{"browser-operator", "automation"},
		{"documents", "artifacts"},
		{"presentations", "artifacts"},
		{"data-analyst", "data_analysis"},
		{"integration-engineer", "integration"},
		{"operations", "operations"},
		{"security", "security"},
		{"product-designer", "ui"},
		{"motion-designer", "ui"},
		{"visual-qa", "ui"},
		{"template-builder", "artifacts"},
	} {
		t.Run(tc.role, func(t *testing.T) {
			t.Parallel()
			ids := SelectProtocols("", tc.role)
			require.Contains(t, ids, tc.protocol)
			body, err := RenderProtocols(ids)
			require.NoError(t, err)
			require.Contains(t, body, "<task_protocol name=\""+tc.protocol+"\">")
			if tc.protocol != "automation" {
				require.NotContains(t, ids, "automation")
			}
		})
	}
	require.Empty(t, SelectProtocols("Analysis of a punctuation typo", ""))
}

func TestBuiltPromptLoadsOnlySelectedGuidance(t *testing.T) {
	root := t.TempDir()
	store := newPromptTestConfig(t, root)
	p, err := NewPrompt("coder", string(loadCoderTemplate(t)), WithWorkingDir(root))
	require.NoError(t, err)
	base, err := p.Build(t.Context(), "openai", "fixture", store)
	require.NoError(t, err)
	ctx := WithProtocols(t.Context(), []string{"ui", "migration"})
	expanded, err := p.Build(ctx, "openai", "fixture", store)
	require.NoError(t, err)
	require.NotContains(t, base, "<task_protocol")
	require.Contains(t, expanded, `<task_protocol name="ui">`)
	require.Contains(t, expanded, `<task_protocol name="migration">`)
	require.Contains(t, base, "<working_contract>")
	require.Contains(t, expanded, "<working_contract>")
	require.Greater(t, len(expanded), len(base))
}
