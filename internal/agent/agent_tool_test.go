package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/credentials"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

// hermeticSubagentCoordinator builds a coordinator against two fully local,
// never-dialed openai-typed providers ("mock" -- the session's primary --
// and "role-provider" -- what a subagent's model role points at), mirroring
// the hermetic setup in coordinator_readiness_test.go. No network call is
// ever made: buildProvider only constructs a client, and the tests below
// never call Run.
func hermeticSubagentCoordinator(t *testing.T) *coordinator {
	t.Helper()
	env := testEnv(t)

	atlasJSON := `{
  "options": {"disable_default_providers": true, "disable_provider_auto_update": true},
  "providers": {
    "mock": {"id": "mock", "name": "Mock", "type": "openai",
      "base_url": "http://127.0.0.1:9/v1", "api_key": "test-key",
      "models": [{"id": "mock-model", "name": "Mock", "context_window": 8192, "default_max_tokens": 128}]},
    "role-provider": {"id": "role-provider", "name": "Role", "type": "openai",
      "base_url": "http://127.0.0.1:9/v1", "api_key": "test-key",
      "models": [{"id": "role-model", "name": "Role", "context_window": 8192, "default_max_tokens": 128}]}
  },
  "models": {"large": {"provider": "mock", "model": "mock-model"},
             "small": {"provider": "mock", "model": "mock-model"}}
}`
	require.NoError(t, os.WriteFile(filepath.Join(env.workingDir, "atlas.json"), []byte(atlasJSON), 0o644))

	cfg, err := config.Init(env.workingDir, "", false)
	require.NoError(t, err)
	cfg.SetupAgents()

	return &coordinator{
		cfg:         cfg,
		sessions:    env.sessions,
		messages:    env.messages,
		permissions: env.permissions,
		history:     env.history,
		filetracker: *env.filetracker,
		credentials: credentials.New(),
	}
}

func TestBuildAdvisorDisabledByDefault(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	model, tools := coord.buildAdvisor(t.Context())
	require.Nil(t, model)
	require.Nil(t, tools)
}

func TestBuildAdvisorWithNoModelRoleConfigured(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	coord.cfg.Config().Options.Advisor = &config.Advisor{Enabled: true}

	model, tools := coord.buildAdvisor(t.Context())
	require.Nil(t, model)
	require.Nil(t, tools)
}

func TestBuildAdvisorResolvesItsModelRole(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	coord.cfg.Config().Options.Advisor = &config.Advisor{Enabled: true}
	coord.cfg.Config().Options.ModelRoles = map[string]config.SelectedModel{
		"advisor": {Provider: "role-provider", Model: "role-model"},
	}

	model, tools := coord.buildAdvisor(t.Context())
	require.NotNil(t, model)
	require.Equal(t, "role-provider", model.ModelCfg.Provider)
	require.Equal(t, "role-model", model.ModelCfg.Model)
	require.NotEmpty(t, tools)

	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Info().Name
	}
	for _, want := range []string{"glob", "grep", "ls", "view"} {
		require.Contains(t, names, want)
	}
	require.NotContains(t, names, "bash", "the advisor must not get write/execute tools")
	require.NotContains(t, names, "edit", "the advisor must not get write/execute tools")
}

func TestBuildEscalatorDisabledByDefault(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	coord.cfg.Config().Options.Advisor = &config.Advisor{Enabled: true}
	advisorModel, advisorTools := coord.buildAdvisor(t.Context())

	model, tools := coord.buildEscalator(t.Context(), advisorModel, advisorTools)
	require.Nil(t, model)
	require.Nil(t, tools)
}

func TestBuildEscalatorFallsBackToAdvisorModelWithNoEscalateRole(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	coord.cfg.Config().Options.Advisor = &config.Advisor{Enabled: true, AutoEscalate: true}
	coord.cfg.Config().Options.ModelRoles = map[string]config.SelectedModel{
		"advisor": {Provider: "role-provider", Model: "role-model"},
	}
	advisorModel, advisorTools := coord.buildAdvisor(t.Context())
	require.NotNil(t, advisorModel, "the advisor must have built for this test to be meaningful")

	model, tools := coord.buildEscalator(t.Context(), advisorModel, advisorTools)
	require.Same(t, advisorModel, model, "no escalate role configured must fall back to the advisor's own model")
	require.Equal(t, len(advisorTools), len(tools))
}

func TestBuildEscalatorResolvesItsOwnRole(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	coord.cfg.Config().Options.Advisor = &config.Advisor{Enabled: true, AutoEscalate: true}
	coord.cfg.Config().Options.ModelRoles = map[string]config.SelectedModel{
		"advisor":  {Provider: "role-provider", Model: "role-model"},
		"escalate": {Provider: "mock", Model: "mock-model"},
	}
	advisorModel, advisorTools := coord.buildAdvisor(t.Context())
	require.NotNil(t, advisorModel)

	model, _ := coord.buildEscalator(t.Context(), advisorModel, advisorTools)
	require.NotNil(t, model)
	require.NotSame(t, advisorModel, model, "a configured escalate role must not reuse the advisor's model")
	require.Equal(t, "mock", model.ModelCfg.Provider)
	require.Equal(t, "mock-model", model.ModelCfg.Model)
}

func TestResolveModelBuildsAReadyModel(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)

	model, err := coord.resolveModel(t.Context(), config.SelectedModel{Provider: "role-provider", Model: "role-model"}, true)
	require.NoError(t, err)
	require.Equal(t, "role-provider", model.ModelCfg.Provider)
	require.Equal(t, "role-model", model.ModelCfg.Model)
}

func TestResolveModelUnknownProvider(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)

	_, err := coord.resolveModel(t.Context(), config.SelectedModel{Provider: "no-such-provider", Model: "x"}, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}

func TestResolveModelUnknownModelInCatalog(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)

	_, err := coord.resolveModel(t.Context(), config.SelectedModel{Provider: "role-provider", Model: "no-such-model"}, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "catalog")
}

func TestBuildSubagentSessionAgentUsesDefaultModelWhenNoRoleSet(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	taskCfg := coord.cfg.Config().Agents[config.AgentTask]

	agent, err := coord.buildSubagentSessionAgent(t.Context(), taskCfg, &subagents.Subagent{Name: "generic", Description: "d"})
	require.NoError(t, err)
	require.Equal(t, "mock-model", agent.Model().ModelCfg.Model)
}

func TestBuildSubagentSessionAgentUsesRoleOverride(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	coord.cfg.Config().Options.ModelRoles = map[string]config.SelectedModel{
		"research": {Provider: "role-provider", Model: "role-model"},
	}
	taskCfg := coord.cfg.Config().Agents[config.AgentTask]

	agent, err := coord.buildSubagentSessionAgent(t.Context(), taskCfg,
		&subagents.Subagent{Name: "research", Description: "d", Model: "@research", Instructions: "Dig deep."})
	require.NoError(t, err)
	require.Equal(t, "role-model", agent.Model().ModelCfg.Model)
	require.Equal(t, "role-provider", agent.Model().ModelCfg.Provider)

	// Built synchronously (unlike the generic task agent's readyWg-based
	// build), so the system prompt is set immediately -- no need to wait
	// for a background goroutine to observe the instructions appended.
	sa, ok := agent.(*sessionAgent)
	require.True(t, ok)
	require.Contains(t, sa.systemPrompt.Get(), "Dig deep.")
	require.Contains(t, sa.systemPrompt.Get(), `name="research"`)
}

func TestBuildSubagentSessionAgentUnknownRoleErrors(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	taskCfg := coord.cfg.Config().Agents[config.AgentTask]

	_, err := coord.buildSubagentSessionAgent(t.Context(), taskCfg,
		&subagents.Subagent{Name: "frontend", Description: "d", Model: "@frontend"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `needs the "@frontend" model role assigned`)
	require.Contains(t, err.Error(), `role "frontend"`, "the suggested set_role call must strip the @ -- set_role's own role param does not take one")
}

// A configured subagent must actually be able to do the work it was
// defined for -- bash, edit, write, and so on -- not be limited to the
// generic task agent's read-only search tools. See subagentAllowedTools.
func TestBuildSubagentSessionAgentGetsFullToolsByDefault(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	taskCfg := coord.cfg.Config().Agents[config.AgentTask]

	agent, err := coord.buildSubagentSessionAgent(t.Context(), taskCfg, &subagents.Subagent{Name: "generic", Description: "d"})
	require.NoError(t, err)

	sa, ok := agent.(*sessionAgent)
	require.True(t, ok)
	names := toolNames(sa.tools.Copy())
	require.Contains(t, names, "bash", "a subagent must be able to run commands, not just search")
	require.Contains(t, names, "edit", "a subagent must be able to edit files, not just search")
	require.Contains(t, names, "write")
}

// Without a depth limit on delegation, giving every subagent the tools
// that spawn further subagents by default would let one invoke itself
// (directly, or through another subagent that calls back). See
// subagentSpawningTools.
func TestBuildSubagentSessionAgentExcludesSpawningToolsByDefault(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	taskCfg := coord.cfg.Config().Agents[config.AgentTask]

	agent, err := coord.buildSubagentSessionAgent(t.Context(), taskCfg, &subagents.Subagent{Name: "generic", Description: "d"})
	require.NoError(t, err)

	sa, ok := agent.(*sessionAgent)
	require.True(t, ok)
	names := toolNames(sa.tools.Copy())
	for _, spawner := range []string{"agent", "delegate", "orchestrate", "debate"} {
		require.NotContains(t, names, spawner, "a subagent must not spawn further subagents unless it explicitly asks for that tool")
	}
}

// A subagent that lists its own "tools" gets exactly that list -- including
// a spawning tool, if it explicitly asked for one.
func TestBuildSubagentSessionAgentHonorsExplicitToolsList(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	taskCfg := coord.cfg.Config().Agents[config.AgentTask]

	agent, err := coord.buildSubagentSessionAgent(t.Context(), taskCfg,
		&subagents.Subagent{Name: "reviewer", Description: "d", Tools: []string{"view", "grep", "agent"}})
	require.NoError(t, err)

	sa, ok := agent.(*sessionAgent)
	require.True(t, ok)
	names := toolNames(sa.tools.Copy())
	require.ElementsMatch(t, []string{"view", "grep", "agent"}, names)
}

func toolNames(tools []fantasy.AgentTool) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Info().Name
	}
	return names
}

func TestResolveSubagentCachesTheBuiltAgent(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	taskCfg := coord.cfg.Config().Agents[config.AgentTask]
	discovered := []*subagents.Subagent{{Name: "generic", Description: "d"}}
	cache := csync.NewMap[string, SessionAgent]()

	first, err := coord.resolveSubagent(t.Context(), taskCfg, discovered, cache, "generic")
	require.NoError(t, err)
	second, err := coord.resolveSubagent(t.Context(), taskCfg, discovered, cache, "generic")
	require.NoError(t, err)

	require.Same(t, first, second, "the second call must reuse the cached instance, not rebuild")
}

// agentToolCallTestContext returns a context carrying real session and
// message IDs (runAgentToolCall/runSubAgent need both) plus a freshly
// created parent session, without ever touching a real provider.
func agentToolCallTestContext(t *testing.T, coord *coordinator) context.Context {
	t.Helper()
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "session-1")
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, "message-1")
	parentSession, err := coord.sessions.Create(ctx, "Parent")
	require.NoError(t, err)
	return context.WithValue(ctx, tools.SessionIDContextKey, parentSession.ID)
}

// TestAgentToolAutoRoutesToTheBestMatchingSubagent drives
// runAgentToolCall directly (bypassing the tool.Run/JSON layer, which
// TestOrchestrateToolRunsNamedAgentsInParallel already exercises for the
// sibling orchestrate tool) with "research" pre-populated in
// subagentInstances -- a cache hit inside resolveSubagent, so this
// never resolves a real model or makes a network call, the same trick
// TestRunOrchestratedAgentHappyPath uses.
func TestAgentToolAutoRoutesToTheBestMatchingSubagent(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})
	ctx := agentToolCallTestContext(t, coord)

	discovered := []*subagents.Subagent{
		{Name: "frontend", Description: "React and CSS component work."},
		{Name: "research", Description: "Deep research into unfamiliar libraries and APIs."},
	}
	mock := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return agentResultWithText("researched it"), nil
	})
	cache := csync.NewMap[string, SessionAgent]()
	cache.Set("research", mock)

	resp, err := coord.runAgentToolCall(ctx, agentToolCallParams{
		discovered:        discovered,
		subagentInstances: cache,
		limiter:           newConcurrencyLimiter(0),
		sessionID:         tools.GetSessionFromContext(ctx),
		agentMessageID:    "message-1",
		toolCallID:        "call-1",
		auto:              true,
		prompt:            "Research the best library for parsing CSV files",
	})
	require.NoError(t, err)
	require.Equal(t, "researched it", resp.Content)

	var meta AgentResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.Equal(t, "research", meta.RoutedTo)
}

func TestAgentToolAutoWithNoMatchFallsBackToTheDefaultAgent(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})
	ctx := agentToolCallTestContext(t, coord)

	discovered := []*subagents.Subagent{{Name: "frontend", Description: "React and CSS component work."}}
	var ranOnDefault bool
	defaultAgent := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		ranOnDefault = true
		return agentResultWithText("default agent answer"), nil
	})

	resp, err := coord.runAgentToolCall(ctx, agentToolCallParams{
		discovered:        discovered,
		subagentInstances: csync.NewMap[string, SessionAgent](),
		defaultAgent:      defaultAgent,
		limiter:           newConcurrencyLimiter(0),
		sessionID:         tools.GetSessionFromContext(ctx),
		agentMessageID:    "message-1",
		toolCallID:        "call-1",
		auto:              true,
		prompt:            "Investigate a completely unrelated database migration failure",
	})
	require.NoError(t, err)
	require.True(t, ranOnDefault, "no keyword match must fall back to the default agent")
	require.Equal(t, "default agent answer", resp.Content)
	require.Empty(t, resp.Metadata, "no match means no routing metadata, same as a plain agent_name-less call")
}

func TestAgentToolAutoIsIgnoredWhenAgentNameIsSet(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})
	ctx := agentToolCallTestContext(t, coord)

	discovered := []*subagents.Subagent{
		{Name: "frontend", Description: "React and CSS component work."},
		{Name: "research", Description: "Deep research into unfamiliar libraries and APIs."},
	}
	frontendMock := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return agentResultWithText("frontend answer"), nil
	})
	cache := csync.NewMap[string, SessionAgent]()
	cache.Set("frontend", frontendMock)

	resp, err := coord.runAgentToolCall(ctx, agentToolCallParams{
		discovered:        discovered,
		subagentInstances: cache,
		limiter:           newConcurrencyLimiter(0),
		sessionID:         tools.GetSessionFromContext(ctx),
		agentMessageID:    "message-1",
		toolCallID:        "call-1",
		agentName:         "frontend",
		auto:              true, // must be ignored: agent_name wins
		prompt:            "Research the best library for parsing CSV files",
	})
	require.NoError(t, err)
	require.Equal(t, "frontend answer", resp.Content, "an explicit agent_name must win over auto-routing")
	require.Empty(t, resp.Metadata, "an explicit agent_name carries no routing metadata")
}

func TestResolveSubagentUnknownNameErrors(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	taskCfg := coord.cfg.Config().Agents[config.AgentTask]
	cache := csync.NewMap[string, SessionAgent]()

	_, err := coord.resolveSubagent(t.Context(), taskCfg, nil, cache, "nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), `no subagent named "nope"`)
}
