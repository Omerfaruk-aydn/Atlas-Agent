package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/prompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/script"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

//go:embed templates/script_tool.md
var scriptToolDescription string

// ScriptToolName is the tool that runs an orchestration program.
const ScriptToolName = "script"

// maxScriptAgents is the most agents one script run may start, however high
// the caller sets max_agents.
const maxScriptAgents = 64

type ScriptParams struct {
	Code           string `json:"code" description:"Starlark program. It must define main(), which returns the result. Top-level code only defines things. Builtins: agent(prompt, agent_name=\"\", schema=None), parallel([lambda: agent(...), ...]), log(...), phase(name), agents_used(), json, struct."`
	MaxAgents      int    `json:"max_agents,omitempty" description:"Most agents the script may start (default 16, max 64). Reaching it stops the script with an error."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" description:"Wall-clock limit for the whole script in seconds (default 1800)."`
}

type ScriptResponseMetadata struct {
	Agents int `json:"agents"`
}

// scriptHost serves a script's agent() calls with this coordinator's
// sub-agent machinery, so a scripted agent is an ordinary sub-agent: same
// limits, same permissions, same cost accounting, same team board.
type scriptHost struct {
	c                 *coordinator
	agentCfg          config.Agent
	discovered        []*subagents.Subagent
	subagentInstances *csync.Map[string, SessionAgent]
	limiter           *concurrencyLimiter
	defaultAgent      func(context.Context) (SessionAgent, error)
	sessionID         string
	agentMessageID    string
	callID            string
	count             atomic.Int64
}

func (h *scriptHost) RunAgent(ctx context.Context, req script.AgentRequest) (script.AgentResult, error) {
	var runAgent SessionAgent
	var err error
	name := req.Agent
	if name != "" {
		runAgent, err = h.c.resolveSubagent(ctx, h.agentCfg, h.discovered, h.subagentInstances, name)
	} else {
		runAgent, err = h.defaultAgent(ctx)
		name = "agent"
	}
	if err != nil {
		return script.AgentResult{}, err
	}
	var schema *resultSchema
	if req.Schema != nil {
		if schema, err = compileResultSchema(req.Schema); err != nil {
			return script.AgentResult{}, err
		}
	}
	if err := h.limiter.acquire(ctx); err != nil {
		return script.AgentResult{}, err
	}
	defer h.limiter.release()

	n := h.count.Add(1)
	resp, err := h.c.runSubAgent(ctx, subAgentParams{
		Agent:          runAgent,
		SessionID:      h.sessionID,
		AgentMessageID: h.agentMessageID,
		ToolCallID:     fmt.Sprintf("%s-script-%d", h.callID, n),
		Prompt:         req.Prompt,
		SessionTitle:   fmt.Sprintf("%s agent session", name),
		BoardName:      name,
		ResultSchema:   schema,
	})
	if err != nil {
		return script.AgentResult{}, err
	}
	if resp.IsError {
		return script.AgentResult{}, errors.New(resp.Content)
	}
	out := script.AgentResult{Text: resp.Content}
	if schema != nil {
		if err := json.Unmarshal([]byte(resp.Content), &out.Value); err != nil {
			return script.AgentResult{}, fmt.Errorf("validated reply is not JSON: %w", err)
		}
	}
	return out, nil
}

// scriptTool lets a model write one program instead of many tool calls. Each
// agent() in the program is an ordinary sub-agent run (see scriptHost), and
// the program itself is hermetic Starlark with hard limits (internal/script).
func (c *coordinator) scriptTool(ctx context.Context) (fantasy.AgentTool, error) {
	agentCfg, ok := c.cfg.Config().Agents[config.AgentTask]
	if !ok {
		return nil, errors.New("task agent not configured")
	}
	taskPromptTemplate, err := taskPrompt(prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}
	opts := c.cfg.Config().Options
	if opts == nil {
		opts = &config.Options{}
	}
	discovered := subagents.Discover(opts.SubagentsPaths)
	subagentInstances := csync.NewMap[string, SessionAgent]()
	limiter := c.sharedSubagentLimiter(opts.MaxConcurrentSubAgents)

	// The default agent is built on first use, not when the tool is
	// assembled: most scripts name specialists and never need it.
	var built SessionAgent
	var buildErr error
	var built1 atomic.Bool
	defaultAgent := func(ctx context.Context) (SessionAgent, error) {
		if built1.CompareAndSwap(false, true) {
			built, buildErr = c.buildAgent(ctx, taskPromptTemplate, agentCfg, true)
		}
		return built, buildErr
	}

	return fantasy.NewAgentTool(
		ScriptToolName,
		scriptToolDescription+describeConfiguredSubagents(discovered),
		func(ctx context.Context, params ScriptParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if strings.TrimSpace(params.Code) == "" {
				return fantasy.NewTextErrorResponse("code is required"), nil
			}
			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}
			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, errors.New("agent message id missing from context")
			}
			host := &scriptHost{
				c: c, agentCfg: agentCfg, discovered: discovered, subagentInstances: subagentInstances,
				limiter: limiter, defaultAgent: defaultAgent,
				sessionID: sessionID, agentMessageID: agentMessageID, callID: call.ID,
			}
			limits := script.Limits{MaxAgents: min(params.MaxAgents, maxScriptAgents)}
			if params.TimeoutSeconds > 0 {
				limits.Timeout = time.Duration(params.TimeoutSeconds) * time.Second
			}
			res, err := script.Run(ctx, params.Code, host, limits)
			meta := ScriptResponseMetadata{Agents: res.Agents}
			if err != nil {
				if ctx.Err() != nil {
					return fantasy.ToolResponse{}, ctx.Err()
				}
				return fantasy.WithResponseMetadata(fantasy.NewTextErrorResponse(scriptFailure(err, res)), meta), nil
			}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(scriptReport(res)), meta), nil
		},
	), nil
}

// scriptReport is the answer a model reads: the result first, then how it was
// reached.
func scriptReport(res script.Result) string {
	var b strings.Builder
	b.WriteString(res.Value)
	if res.Log != "" {
		b.WriteString("\n\n--- script log ---\n" + res.Log)
	}
	fmt.Fprintf(&b, "\n\n[%d agent(s) ran]", res.Agents)
	return b.String()
}

func scriptFailure(err error, res script.Result) string {
	var b strings.Builder
	b.WriteString("The script failed: " + err.Error())
	if errors.Is(err, script.ErrBudget) {
		b.WriteString("\nRaise max_agents or timeout_seconds, or split the work.")
	}
	if res.Log != "" {
		b.WriteString("\n\n--- script log ---\n" + res.Log)
	}
	fmt.Fprintf(&b, "\n\n[%d agent(s) ran before it stopped; their work is not lost]", res.Agents)
	return b.String()
}
