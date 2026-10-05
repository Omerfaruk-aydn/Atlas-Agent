package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/prompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

func TestNewSpecialistInheritsModelAndLoadsBoundProcedures(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	taskCfg := coord.cfg.Config().Agents[config.AgentTask]
	role, ok := subagents.Find(subagents.Builtin(), "documents")
	require.True(t, ok)
	a, err := coord.buildSubagentSessionAgent(t.Context(), taskCfg, role)
	require.NoError(t, err)
	require.Equal(t, "mock-model", a.Model().ModelCfg.Model)
	content := a.(*sessionAgent).systemPrompt.Get()
	require.Contains(t, content, `name="office-documents" status="loaded"`)
	require.Contains(t, content, "Reopen or parse the final file independently")
	require.NotContains(t, content, `name="desktop-automation" status="loaded"`)
	coord.cfg.Config().Options.ModelRoles = map[string]config.SelectedModel{
		"documents": {Provider: "role-provider", Model: "role-model"},
	}
	a, err = coord.buildSubagentSessionAgent(t.Context(), taskCfg, role)
	require.NoError(t, err)
	require.Equal(t, "role-model", a.Model().ModelCfg.Model)
	coord.cfg.Config().Options.ModelRoles["documents"] = config.SelectedModel{Provider: "missing-provider", Model: "missing-model"}
	_, err = coord.buildSubagentSessionAgent(t.Context(), taskCfg, role)
	require.Error(t, err, "An invalid explicit assignment must not silently inherit")
}

func TestInheritedSessionModeRejectsBrokenExplicitAssignment(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	coord.cfg.Config().Options.SessionMode = "documents"
	_, selected, err := coord.sessionModeModel(t.Context())
	require.NoError(t, err)
	require.False(t, selected)
	coord.cfg.Config().Options.ModelRoles = map[string]config.SelectedModel{
		"documents": {Provider: "missing-provider", Model: "missing-model"},
	}
	_, _, err = coord.sessionModeModel(t.Context())
	require.Error(t, err)
}

func TestSessionModeAndDelegationSelectTheSameSkillRecipe(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	coord.cfg.Config().Options.SessionMode = "documents"
	p, err := prompt.NewPrompt("fixture", "{{.RoleSkillGuidance}}")
	require.NoError(t, err)
	startup, err := p.Build(coord.withModeSkills(t.Context()), "mock", "fixture", coord.cfg)
	require.NoError(t, err)
	ctx, err := coord.preparePromptContext(t.Context(), "fixture", "devam et")
	require.NoError(t, err)
	turn, err := p.Build(ctx, "mock", "fixture", coord.cfg)
	require.NoError(t, err)
	require.Equal(t, startup, turn)
	require.Contains(t, turn, `name="office-documents" status="loaded"`)
	coord.cfg.Config().Options.DisabledSkills = []string{"office-documents"}
	disabled, err := p.Build(coord.withModeSkills(t.Context()), "mock", "fixture", coord.cfg)
	require.NoError(t, err)
	require.Contains(t, disabled, `name="office-documents" status="unavailable"`)
	require.NotContains(t, disabled, "Reopen or parse the final file independently")
}
