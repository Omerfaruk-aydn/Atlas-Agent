package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/prompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

//go:embed templates/agent_tool.md
var agentToolDescription string

type AgentParams struct {
	Prompt string `json:"prompt" description:"The task for the agent to perform"`
	// AgentName optionally names a configured subagent (see
	// internal/subagents and `atlas agent list`) to run the task with,
	// instead of the default agent.
	AgentName string `json:"agent_name,omitempty" description:"Name of a configured subagent to hand this task to, instead of the default agent"`
	// Auto, when true and agent_name is empty, picks the best-matching
	// configured subagent automatically instead of naming one -- see
	// subagents.Match for how "best-matching" is decided.
	Auto bool `json:"auto,omitempty" description:"When true and agent_name is empty, automatically route to whichever configured subagent's description best matches this prompt by keyword overlap, instead of running the default agent. Falls back to the default agent if no subagent matches. Ignored when agent_name is set."`
}

// AgentResponseMetadata reports which agent actually ran, useful only
// for the auto-routing path: agent_name is an explicit choice the
// caller already knows, but an auto-routed call only becomes
// transparent if the response says which subagent it landed on.
type AgentResponseMetadata struct {
	// RoutedTo is the auto-selected subagent's name, or empty when auto
	// routing was not requested, found no match, or agent_name was set
	// explicitly instead.
	RoutedTo string `json:"routed_to,omitempty"`
}

const (
	AgentToolName = "agent"
)

func (c *coordinator) agentTool(ctx context.Context) (fantasy.AgentTool, error) {
	agentCfg, ok := c.cfg.Config().Agents[config.AgentTask]
	if !ok {
		return nil, errors.New("task agent not configured")
	}
	taskPromptTemplate, err := taskPrompt(prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}

	agent, err := c.buildAgent(ctx, taskPromptTemplate, agentCfg, true)
	if err != nil {
		return nil, err
	}

	// Subagents are discovered once per tool build (session start), not
	// per call: discovery reads disk, and a subagent's definition is not
	// expected to change mid-session. Each named subagent's SessionAgent
	// is built lazily on first use and cached for the rest of the
	// session, since building one resolves a provider/model and a system
	// prompt -- worth doing once, not on every call.
	var opts *config.Options
	if opts = c.cfg.Config().Options; opts == nil {
		opts = &config.Options{}
	}
	discovered := subagents.Discover(opts.SubagentsPaths)
	subagentInstances := csync.NewMap[string, SessionAgent]()

	// One limiter for the tool, not per call: the point is to bound the
	// sub-agents running across all of this tool's concurrent calls.
	limiter := newConcurrencyLimiter(c.cfg.Config().Options.MaxConcurrentSubAgents)

	return fantasy.NewParallelAgentTool(
		AgentToolName,
		agentToolDescription+describeConfiguredSubagents(discovered),
		func(ctx context.Context, params AgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Prompt == "" {
				return fantasy.NewTextErrorResponse("prompt is required"), nil
			}

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}

			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, errors.New("agent message id missing from context")
			}

			return c.runAgentToolCall(ctx, agentToolCallParams{
				agentCfg:          agentCfg,
				discovered:        discovered,
				subagentInstances: subagentInstances,
				defaultAgent:      agent,
				limiter:           limiter,
				sessionID:         sessionID,
				agentMessageID:    agentMessageID,
				toolCallID:        call.ID,
				agentName:         params.AgentName,
				auto:              params.Auto,
				prompt:            params.Prompt,
			})
		},
	), nil
}

// agentToolCallParams holds everything runAgentToolCall needs to resolve
// which agent a call should run on and run it. Splitting this out of the
// tool closure lets a test drive routing decisions (agent_name, auto)
// against an in-memory discovered list and a pre-populated
// subagentInstances cache, without ever resolving a real model or
// making a network call -- the same trick TestRunOrchestratedAgentHappyPath
// uses for orchestrate.
type agentToolCallParams struct {
	agentCfg          config.Agent
	discovered        []*subagents.Subagent
	subagentInstances *csync.Map[string, SessionAgent]
	defaultAgent      SessionAgent
	limiter           *concurrencyLimiter
	sessionID         string
	agentMessageID    string
	toolCallID        string
	agentName         string
	auto              bool
	prompt            string
}

func (c *coordinator) runAgentToolCall(ctx context.Context, p agentToolCallParams) (fantasy.ToolResponse, error) {
	runAgent := p.defaultAgent
	sessionTitle := "New Agent Session"
	routedTo := ""

	switch {
	case p.agentName != "":
		resolved, err := c.resolveSubagent(ctx, p.agentCfg, p.discovered, p.subagentInstances, p.agentName)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		runAgent = resolved
		sessionTitle = p.agentName + " agent session"
	case p.auto:
		if matched, ok := subagents.Match(p.discovered, p.prompt); ok {
			resolved, err := c.resolveSubagent(ctx, p.agentCfg, p.discovered, p.subagentInstances, matched.Name)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			runAgent = resolved
			sessionTitle = matched.Name + " agent session (auto-routed)"
			routedTo = matched.Name
		}
		// No match: silently falls back to the default agent, same as
		// an empty agent_name would.
	}

	if err := p.limiter.acquire(ctx); err != nil {
		return fantasy.ToolResponse{}, err
	}
	defer p.limiter.release()

	resp, err := c.runSubAgent(ctx, subAgentParams{
		Agent:          runAgent,
		SessionID:      p.sessionID,
		AgentMessageID: p.agentMessageID,
		ToolCallID:     p.toolCallID,
		Prompt:         p.prompt,
		SessionTitle:   sessionTitle,
	})
	if err != nil || routedTo == "" {
		return resp, err
	}
	return fantasy.WithResponseMetadata(resp, AgentResponseMetadata{RoutedTo: routedTo}), nil
}

// resolveSubagent returns the cached SessionAgent for a named subagent,
// building and caching it on first use. Unlike buildAgent (used for the
// generic task agent at session startup, where hiding latency behind
// readyWg goroutines is worth the complexity), this builds synchronously:
// it only runs the first time a given subagent is actually invoked in a
// session, not on every session's startup.
func (c *coordinator) resolveSubagent(
	ctx context.Context,
	taskCfg config.Agent,
	discovered []*subagents.Subagent,
	cache *csync.Map[string, SessionAgent],
	name string,
) (SessionAgent, error) {
	if cached, ok := cache.Get(name); ok {
		return cached, nil
	}

	sub, ok := subagents.Find(discovered, name)
	if !ok {
		return nil, fmt.Errorf("no subagent named %q is configured; see the list of configured subagents at the end of this tool's description", name)
	}

	built, err := c.buildSubagentSessionAgent(ctx, taskCfg, sub)
	if err != nil {
		return nil, err
	}
	cache.Set(name, built)
	return built, nil
}

// buildSubagentSessionAgent builds a dedicated SessionAgent for a named
// subagent: the same tools and defaults as the generic task agent, but with
// the subagent's instructions appended to the system prompt and, if the
// subagent names a model role, running on that model instead of the
// session's primary one.
func (c *coordinator) buildSubagentSessionAgent(ctx context.Context, taskCfg config.Agent, sub *subagents.Subagent) (SessionAgent, error) {
	large, small, largeFallbacks, smallFallbacks, err := c.buildAgentModels(ctx, true)
	if err != nil {
		return nil, err
	}

	if sub.Model != "" {
		modelCfg, ok := c.cfg.Config().ResolveRole(sub.Model)
		if !ok {
			return nil, fmt.Errorf(
				"subagent %q needs the %q model role assigned before it can run; call atlas_config with action \"set_role\" and role %q to assign it a provider and model",
				sub.Name, sub.Model, config.StripRoleReference(sub.Model))
		}
		large, err = c.resolveModel(ctx, modelCfg, true)
		if err != nil {
			return nil, fmt.Errorf("subagent %q: resolving model role %q: %w", sub.Name, sub.Model, err)
		}
		// The role override's own fallback chain, if any, is not modeled
		// here: Options.ModelFallbacks is keyed by "large"/"small", not by
		// custom role name, so there is nothing to look up for it yet.
		largeFallbacks = nil
	}

	taskSystemPrompt, err := taskPrompt(prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}
	systemPrompt, err := taskSystemPrompt.Build(ctx, large.Model.Provider(), large.Model.Model(), c.cfg)
	if err != nil {
		return nil, err
	}
	if sub.Instructions != "" {
		systemPrompt += "\n\n<subagent name=\"" + sub.Name + "\">\n" + sub.Instructions + "\n</subagent>"
	}

	agentTools, err := c.buildTools(ctx, taskCfg, true)
	if err != nil {
		return nil, err
	}

	largeProviderCfg, _ := c.cfg.Config().Providers.Get(large.ModelCfg.Provider)
	return NewSessionAgent(SessionAgentOptions{
		LargeModel:           large,
		LargeModelFallbacks:  largeFallbacks,
		FallbackCooldown:     time.Duration(c.cfg.Config().Options.FallbackCooldown) * time.Second,
		SmallModel:           small,
		SmallModelFallbacks:  smallFallbacks,
		SystemPromptPrefix:   largeProviderCfg.SystemPromptPrefix,
		SystemPrompt:         systemPrompt,
		IsSubAgent:           true,
		DisableAutoSummarize: c.cfg.Config().Options.DisableAutoSummarize,
		AutoSummarizeAt:      c.cfg.Config().Options.AutoSummarizeAt,
		MaxProviderRetries:   c.cfg.Config().Options.MaxProviderRetries,
		MaxSessionCost:       c.cfg.Config().Options.MaxSessionCost,
		MaxStepsPerTurn:      c.cfg.Config().Options.MaxStepsPerTurn,
		PromptHooks:          c.hookRunner(hooks.EventUserPromptSubmit),
		OnProviderExhausted:  c.credentials.Advance,
		IsYolo:               c.permissions.SkipRequests(),
		Permissions:          c.permissions,
		Sessions:             c.sessions,
		Messages:             c.messages,
		Tools:                agentTools,
		Notify:               c.notify,
		RunComplete:          c.runComplete,
	}), nil
}
