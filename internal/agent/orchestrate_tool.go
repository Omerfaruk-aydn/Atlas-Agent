package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

//go:embed templates/orchestrate_tool.md
var orchestrateToolDescription string

const OrchestrateToolName = "orchestrate"

type OrchestrateParams struct {
	Prompt string `json:"prompt" description:"The task to hand to every named agent, verbatim. Each one receives the exact same prompt and runs independently."`
	// AgentNames names at least two configured subagents (see internal/subagents
	// and `atlas agent list`) to run this same prompt with, in parallel.
	AgentNames stringList `json:"agent_names" description:"At least two distinct subagent names to run this prompt with, in parallel."`
	// JudgeAgent optionally names a third, distinct subagent that reads
	// every answer once they are all in and produces one final synthesis
	// -- the best answer, a merge, or its own correction if all of them
	// share a flaw -- instead of leaving that comparison to the caller.
	JudgeAgent string `json:"judge_agent,omitempty" description:"A subagent, distinct from agent_names, that reviews every answer once they are all in and produces one final synthesized answer. Optional -- omit to get the raw answers only."`
}

type OrchestrateResponseMetadata struct {
	Agents []string `json:"agents"`
	Judge  string   `json:"judge,omitempty"`
}

type orchestrateResult struct {
	name    string
	content string
	err     error
}

// orchestrateTool runs the same prompt on several named subagents in
// parallel and returns every answer side by side, so the calling model
// can cross-check them in one round trip instead of orchestrating
// several `agent` calls itself and comparing them by hand. It shares
// resolveSubagent/buildSubagentSessionAgent/runSubAgent with agentTool
// -- the only difference is fanning one prompt out to several agents
// instead of routing it to one.
func (c *coordinator) orchestrateTool(ctx context.Context) (fantasy.AgentTool, error) {
	agentCfg, ok := c.cfg.Config().Agents[config.AgentTask]
	if !ok {
		return nil, errors.New("task agent not configured")
	}

	var opts *config.Options
	if opts = c.cfg.Config().Options; opts == nil {
		opts = &config.Options{}
	}
	discovered := subagents.Discover(opts.SubagentsPaths)
	subagentInstances := csync.NewMap[string, SessionAgent]()

	// A limiter scoped to this tool, the same way agentTool's is: it
	// bounds how many of the agents named in one orchestrate call run at
	// once, using the same MaxConcurrentSubAgents setting.
	limiter := newConcurrencyLimiter(c.cfg.Config().Options.MaxConcurrentSubAgents)

	return fantasy.NewParallelAgentTool(
		OrchestrateToolName,
		orchestrateToolDescription+describeConfiguredSubagents(discovered),
		func(ctx context.Context, params OrchestrateParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if strings.TrimSpace(params.Prompt) == "" {
				return fantasy.NewTextErrorResponse("prompt is required"), nil
			}

			names := dedupeAgentNames(params.AgentNames)
			if len(names) < 2 {
				return fantasy.NewTextErrorResponse(
					"orchestrate needs at least two distinct agent_names -- for a single agent, use `agent` instead"), nil
			}

			judgeName := strings.TrimSpace(params.JudgeAgent)
			if judgeName != "" && slices.Contains(names, judgeName) {
				return fantasy.NewTextErrorResponse(
					"judge_agent must be a different agent from agent_names -- it needs an independent view of the answers, not one it also produced"), nil
			}

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}
			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, errors.New("agent message id missing from context")
			}

			results := make([]orchestrateResult, len(names))
			var wg sync.WaitGroup
			for i, name := range names {
				wg.Add(1)
				go func(i int, name string) {
					defer wg.Done()
					results[i] = c.runOrchestratedAgent(ctx, orchestratedAgentParams{
						agentCfg:          agentCfg,
						discovered:        discovered,
						subagentInstances: subagentInstances,
						limiter:           limiter,
						name:              name,
						sessionID:         sessionID,
						agentMessageID:    agentMessageID,
						toolCallID:        fmt.Sprintf("%s-%s", call.ID, name),
						prompt:            params.Prompt,
					})
				}(i, name)
			}
			wg.Wait()

			var judge *orchestrateResult
			if judgeName != "" {
				res := c.runOrchestratedAgent(ctx, orchestratedAgentParams{
					agentCfg:          agentCfg,
					discovered:        discovered,
					subagentInstances: subagentInstances,
					limiter:           limiter,
					name:              judgeName,
					sessionID:         sessionID,
					agentMessageID:    agentMessageID,
					toolCallID:        fmt.Sprintf("%s-judge-%s", call.ID, judgeName),
					prompt:            buildJudgePrompt(params.Prompt, results),
				})
				judge = &res
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(formatOrchestrateResults(results, judge)),
				OrchestrateResponseMetadata{Agents: names, Judge: judgeName},
			), nil
		},
	), nil
}

type orchestratedAgentParams struct {
	agentCfg          config.Agent
	discovered        []*subagents.Subagent
	subagentInstances *csync.Map[string, SessionAgent]
	limiter           *concurrencyLimiter
	name              string
	sessionID         string
	agentMessageID    string
	toolCallID        string
	prompt            string
}

func (c *coordinator) runOrchestratedAgent(ctx context.Context, p orchestratedAgentParams) orchestrateResult {
	runAgent, err := c.resolveSubagent(ctx, p.agentCfg, p.discovered, p.subagentInstances, p.name)
	if err != nil {
		return orchestrateResult{name: p.name, err: err}
	}

	if err := p.limiter.acquire(ctx); err != nil {
		return orchestrateResult{name: p.name, err: err}
	}
	defer p.limiter.release()

	resp, err := c.runSubAgent(ctx, subAgentParams{
		Agent:          runAgent,
		SessionID:      p.sessionID,
		AgentMessageID: p.agentMessageID,
		ToolCallID:     p.toolCallID,
		Prompt:         p.prompt,
		SessionTitle:   p.name + " agent session",
	})
	if err != nil {
		return orchestrateResult{name: p.name, err: err}
	}
	return orchestrateResult{name: p.name, content: resp.Content}
}

// dedupeAgentNames trims and deduplicates names, preserving first-seen
// order, so a caller listing the same agent twice by mistake gets one
// real run rather than two identical ones counted as corroboration.
func dedupeAgentNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// buildJudgePrompt hands a judge agent the original task and every
// answer produced for it (failures included, so the judge can see that
// an agent came up empty rather than treating its absence as agreement).
func buildJudgePrompt(task string, results []orchestrateResult) string {
	var b strings.Builder
	b.WriteString("You are judging independent answers from several agents given the exact same task. " +
		"Read them, then give ONE final answer: the best one, a merge of their strengths, or your own " +
		"corrected answer if they share a flaw. Note any meaningful disagreement between them.\n\n")
	fmt.Fprintf(&b, "Task:\n%s\n\nAnswers:\n", task)
	for _, r := range results {
		fmt.Fprintf(&b, "\n=== %s ===\n", r.name)
		if r.err != nil {
			fmt.Fprintf(&b, "(failed: %s)\n", r.err)
			continue
		}
		b.WriteString(r.content)
		b.WriteString("\n")
	}
	return b.String()
}

// formatOrchestrateResults renders every agent's raw answer, preceded by
// the judge's synthesis when one was requested. The raw answers stay in
// the response even with a judge present, so a synthesis that misreads
// or drops something stays checkable against the source material.
func formatOrchestrateResults(results []orchestrateResult, judge *orchestrateResult) string {
	var b strings.Builder
	if judge != nil {
		if judge.err != nil {
			fmt.Fprintf(&b, "Synthesis unavailable -- judge %q failed: %s\n\n", judge.name, judge.err)
		} else {
			fmt.Fprintf(&b, "=== Synthesis (judged by %s) ===\n%s\n\n", judge.name, judge.content)
		}
		b.WriteString("--- Raw answers ---\n")
	}

	fmt.Fprintf(&b, "%d agent(s) ran independently on the same prompt.\n", len(results))
	for _, r := range results {
		fmt.Fprintf(&b, "\n=== %s ===\n", r.name)
		if r.err != nil {
			fmt.Fprintf(&b, "failed: %s\n", r.err)
			continue
		}
		b.WriteString(r.content)
		b.WriteString("\n")
	}
	return b.String()
}
