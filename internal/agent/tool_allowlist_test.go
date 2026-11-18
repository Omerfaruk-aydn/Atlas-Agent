package agent

import (
	"slices"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/skills"
	"github.com/stretchr/testify/require"
)

// Every tool the coordinator builds is filtered through the agent's
// AllowedTools before the model sees it, and a name that is not on that list
// is dropped without a word: no error, no log line, just a tool that quietly
// does not exist. Two of them spent the rebrand in that state, listed as
// Atlas-Agent_info and Atlas-Agent_logs, and nothing failed.
func TestEveryBuiltToolIsAllowed(t *testing.T) {
	env := testEnv(t)

	cfg, err := config.Init(env.workingDir, "", false)
	require.NoError(t, err)

	c := &coordinator{
		cfg:          cfg,
		sessions:     env.sessions,
		messages:     env.messages,
		permissions:  env.permissions,
		history:      env.history,
		filetracker:  *env.filetracker,
		memory:       memoryStore(cfg),
		skillTracker: skills.NewTracker(nil),
		interactive:  true,
	}

	// Set the agents up here rather than relying on load having done it:
	// a config reload triggered by a neighbouring test can swap the config
	// pointer for one whose agents have not been built yet.
	cfg.SetupAgents()
	agentCfg, ok := cfg.Config().Agents[config.AgentCoder]
	require.True(t, ok)

	// The two sub-agent tools are built only when they are already allowed,
	// and building them needs a model this test has no reason to configure.
	// They are not where drift hides: everything else is appended
	// unconditionally and then filtered.
	buildable := agentCfg
	buildable.AllowedTools = slices.DeleteFunc(slices.Clone(agentCfg.AllowedTools), func(name string) bool {
		return name == AgentToolName || name == tools.AgenticFetchToolName || name == OrchestrateToolName || name == DelegateToolName || name == VibeToolName
	})

	built, err := c.assembleTools(t.Context(), buildable, false)
	require.NoError(t, err)
	require.NotEmpty(t, built)

	for _, tool := range built {
		require.Contains(t, agentCfg.AllowedTools, tool.Info().Name,
			"the coordinator builds %q but no agent may use it: add it to allToolNames", tool.Info().Name)
	}
}

// The orchestrator mode coordinates and never edits: the main session keeps
// its planning, delegation and board tools, gets the team board without the
// tools.teams switch, and loses every tool that writes or runs commands.
func TestOrchestratorModeNarrowsTheMainSessionTools(t *testing.T) {
	env := testEnv(t)
	cfg, err := config.Init(env.workingDir, "", false)
	require.NoError(t, err)
	c := &coordinator{
		cfg:          cfg,
		sessions:     env.sessions,
		messages:     env.messages,
		permissions:  env.permissions,
		history:      env.history,
		filetracker:  *env.filetracker,
		memory:       memoryStore(cfg),
		skillTracker: skills.NewTracker(nil),
		interactive:  true,
	}
	cfg.SetupAgents()
	cfg.Config().Options.SessionMode = "orchestrator"
	agentCfg := cfg.Config().Agents[config.AgentCoder]

	mode, ok := c.sessionMode()
	require.True(t, ok)
	allowed := subagentAllowedTools(mode, agentCfg.AllowedTools)
	for _, name := range []string{AgentToolName, AgentResultToolName, tools.TeamSendToolName, tools.TeamReadToolName, tools.TodosToolName, "worktree", "view"} {
		require.Contains(t, allowed, name)
	}

	buildable := agentCfg
	buildable.AllowedTools = slices.DeleteFunc(slices.Clone(agentCfg.AllowedTools), func(name string) bool {
		return name == AgentToolName || name == tools.AgenticFetchToolName || name == OrchestrateToolName || name == DelegateToolName || name == VibeToolName
	})
	built, err := c.assembleTools(t.Context(), buildable, false)
	require.NoError(t, err)
	var names []string
	for _, tool := range c.filterTools(built, buildable, nil, nil, false) {
		names = append(names, tool.Info().Name)
	}
	for _, want := range []string{tools.TeamSendToolName, tools.TeamReadToolName, tools.TodosToolName, "view", "grep", GoalToolName} {
		require.Contains(t, names, want)
	}
	for _, banned := range []string{"edit", "multiedit", "write", "bash", "download"} {
		require.NotContains(t, names, banned, "the orchestrator must not %s itself", banned)
	}

	// Without the mode the board tools stay behind their switch.
	cfg.Config().Options.SessionMode = ""
	built, err = c.assembleTools(t.Context(), buildable, false)
	require.NoError(t, err)
	names = names[:0]
	for _, tool := range c.filterTools(built, buildable, nil, nil, false) {
		names = append(names, tool.Info().Name)
	}
	require.Contains(t, names, "edit")
	if !cfg.Config().Tools.Teams.IsEnabled() {
		require.NotContains(t, names, tools.TeamSendToolName)
	}
}
