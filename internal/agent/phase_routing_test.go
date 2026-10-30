package agent

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestWorkPhaseUsesLatestCompletedToolIntent(t *testing.T) {
	t.Parallel()
	require.Equal(t, "planning", workPhase(nil))
	for name, phase := range map[string]string{"view": "exploration", "edit": "implementation", "bash": "verification"} {
		prompt := fantasy.Prompt{{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ToolCallPart{ToolName: name}}}}
		require.Equal(t, phase, workPhase(prompt))
	}
}

func TestPhaseRoutingChangesOnlyForConfiguredModels(t *testing.T) {
	t.Parallel()
	primary := Model{Model: fastModel{}}
	primary.ModelCfg.Provider, primary.ModelCfg.Model = "one", "writer"
	fast := Model{Model: fastModel{}}
	fast.ModelCfg.Provider, fast.ModelCfg.Model, fast.ModelCfg.ReasoningEffort = "two", "reader", "low"
	a := &sessionAgent{stageModels: func(phase string) *Model {
		if phase == "exploration" {
			return &fast
		}
		return nil
	}}
	chain := newModelChain(primary, nil, 0)
	prompt := fantasy.Prompt{{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ToolCallPart{ToolName: "view"}}}}
	a.selectWorkPhase(chain, primary, prompt, "s")
	require.Equal(t, "reader", chain.Current().ModelCfg.Model)
	require.Equal(t, 0, chain.active)
	a.selectWorkPhase(chain, primary, prompt, "s")
	require.Equal(t, "reader", chain.Current().ModelCfg.Model)
	a.selectWorkPhase(chain, primary, nil, "s")
	require.Equal(t, "writer", chain.Current().ModelCfg.Model)
}

func TestDifferentProviderRolePrefersAdvisorThenDeterministicRole(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Options: &config.Options{ModelRoles: map[string]config.SelectedModel{
		"zeta":      {Provider: "two", Model: "z"},
		"advisor":   {Provider: "two", Model: "a"},
		"alpha":     {Provider: "two", Model: "b"},
		"same":      {Provider: "one", Model: "c"},
		"evaluator": {Provider: "three", Model: "explicit"},
	}}}
	advisor := &Model{}
	advisor.ModelCfg.Provider = "two"
	role, ok := differentProviderRole(cfg, "one", advisor)
	require.True(t, ok)
	require.Equal(t, "advisor", role)
	delete(cfg.Options.ModelRoles, "advisor")
	role, ok = differentProviderRole(cfg, "one", nil)
	require.True(t, ok)
	require.Equal(t, "alpha", role)
	sameOnly := &config.Config{Options: &config.Options{ModelRoles: map[string]config.SelectedModel{"same": {Provider: "one", Model: "c"}}}}
	_, ok = differentProviderRole(sameOnly, "one", nil)
	require.False(t, ok)
}

func TestOracleBudgetIsBoundedAndResets(t *testing.T) {
	t.Parallel()
	c := &coordinator{}
	now := time.Now()
	for range oracleMaxCallsPerHour {
		require.True(t, c.reserveOracle("s", now))
	}
	require.False(t, c.reserveOracle("s", now.Add(time.Minute)))
	require.True(t, c.reserveOracle("s", now.Add(time.Hour)))
	require.True(t, c.reserveOracle("other", now))
}
