package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageProfileRestrictsToolsAndMCP(t *testing.T) {
	t.Parallel()
	cfg := &Config{Options: &Options{}}
	require.NoError(t, cfg.ApplyUsageProfile("review"))
	cfg.SetupAgents()
	require.NotContains(t, cfg.Agents[AgentCoder].AllowedTools, "bash")
	require.NotContains(t, cfg.Agents[AgentCoder].AllowedTools, "edit")
	require.Contains(t, cfg.Agents[AgentCoder].AllowedTools, "view")
	require.NotNil(t, cfg.Agents[AgentCoder].AllowedMCP)
	require.Equal(t, 48, cfg.Options.MaxStepsPerTurn)
}

func TestUsageProfileSurvivesReloadAndExplicitSelectionReplacesOverride(t *testing.T) {
	dir := t.TempDir()
	store, err := Load(dir, dir, false)
	require.NoError(t, err)
	require.NoError(t, store.OverrideUsageProfile("review"))
	require.NoError(t, store.ReloadFromDisk(t.Context()))
	require.Equal(t, "review", store.Config().Options.UsageProfile)
	require.Equal(t, 48, store.Config().Options.MaxStepsPerTurn)
	require.NoError(t, store.SetConfigField(ScopeWorkspace, "options.usage_profile", "implementation"))
	require.Equal(t, "implementation", store.Config().Options.UsageProfile)
	require.Equal(t, 96, store.Config().Options.MaxStepsPerTurn)
}

func TestCustomUsageProfileResolvesConfiguredModelRole(t *testing.T) {
	t.Parallel()
	cfg := &Config{Options: &Options{ModelRoles: map[string]SelectedModel{"coding": {Provider: "mock", Model: "configured"}}, UsageProfiles: map[string]UsageProfile{"team": {ModelRole: "coding", MaxSteps: 40, MaxAgents: 2, AllowedTools: []string{"view", "edit"}}}}}
	require.NoError(t, cfg.ApplyUsageProfile("team"))
	cfg.SetupAgents()
	require.Equal(t, "configured", cfg.Models[SelectedModelTypeLarge].Model)
	require.ElementsMatch(t, []string{"view", "edit"}, cfg.Agents[AgentCoder].AllowedTools)
	require.Error(t, cfg.ApplyUsageProfile("unknown"))
}
