package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

//go:embed templates/delegate_tool.md
var delegateToolDescription string

const DelegateToolName = "delegate"

// DelegateTask is one subtask: a named agent and the self-contained
// prompt for its share of the work.
type DelegateTask struct {
	AgentName string `json:"agent_name" description:"A configured subagent (see internal/subagents and atlas agent list) to run this subtask on."`
	Prompt    string `json:"prompt" description:"This subtask's own, self-contained instructions. The agent sees only this prompt, not the other tasks or the overall goal, so include whatever context this piece of work needs."`
}

type DelegateParams struct {
	// Tasks decomposes the overall work: at least two entries, each
	// naming an agent and carrying that subtask's own prompt. Unlike
	// orchestrate, the same agent_name may repeat across tasks.
	Tasks taskList `json:"tasks" description:"At least two subtasks to run in parallel, each with its own agent_name and prompt."`
}

type DelegateResponseMetadata struct {
	Agents []string `json:"agents"`
}

type delegateResult struct {
	agentName string
	prompt    string
	content   string
	err       error
}

// delegateTool splits a task into independent subtasks and runs each on
// its own named agent in parallel -- the decomposition counterpart to
// orchestrateTool's verification-by-consensus (same prompt, several
// agents). It shares resolveSubagent/runSubAgent with agentTool and
// orchestrateTool; the only difference is that each parallel run gets
// its own prompt instead of a shared one.
func (c *coordinator) delegateTool(ctx context.Context) (fantasy.AgentTool, error) {
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

	// A limiter scoped to this tool, the same way orchestrateTool's is:
	// it bounds how many of the tasks in one delegate call run at once,
	// using the same MaxConcurrentSubAgents setting.
	limiter := newConcurrencyLimiter(c.cfg.Config().Options.MaxConcurrentSubAgents)

	return fantasy.NewParallelAgentTool(
		DelegateToolName,
		delegateToolDescription+describeConfiguredSubagents(discovered),
		func(ctx context.Context, params DelegateParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			tasks := validDelegateTasks(params.Tasks)
			if len(tasks) < 2 {
				return fantasy.NewTextErrorResponse(
					"delegate needs at least two tasks, each with a non-empty agent_name and prompt -- for a single task, use `agent` instead"), nil
			}

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}
			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, errors.New("agent message id missing from context")
			}

			results := make([]delegateResult, len(tasks))
			agents := make([]string, len(tasks))
			var wg sync.WaitGroup
			for i, task := range tasks {
				agents[i] = task.AgentName
				wg.Add(1)
				go func(i int, task DelegateTask) {
					defer wg.Done()
					res := c.runOrchestratedAgent(ctx, orchestratedAgentParams{
						agentCfg:          agentCfg,
						discovered:        discovered,
						subagentInstances: subagentInstances,
						limiter:           limiter,
						name:              task.AgentName,
						sessionID:         sessionID,
						agentMessageID:    agentMessageID,
						toolCallID:        fmt.Sprintf("%s-%d-%s", call.ID, i, task.AgentName),
						prompt:            task.Prompt,
					})
					results[i] = delegateResult{
						agentName: task.AgentName,
						prompt:    task.Prompt,
						content:   res.content,
						err:       res.err,
					}
				}(i, task)
			}
			wg.Wait()

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(formatDelegateResults(results)),
				DelegateResponseMetadata{Agents: agents},
			), nil
		},
	), nil
}

// validDelegateTasks drops any task missing an agent_name or prompt,
// rather than failing the whole call over one malformed entry -- the
// same leniency dedupeAgentNames applies to a blank name in
// orchestrate's list.
func validDelegateTasks(tasks []DelegateTask) []DelegateTask {
	out := make([]DelegateTask, 0, len(tasks))
	for _, t := range tasks {
		t.AgentName = strings.TrimSpace(t.AgentName)
		t.Prompt = strings.TrimSpace(t.Prompt)
		if t.AgentName == "" || t.Prompt == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func formatDelegateResults(results []delegateResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d subtask(s) ran in parallel.\n", len(results))
	for i, r := range results {
		fmt.Fprintf(&b, "\n=== Task %d: %s ===\n", i+1, r.agentName)
		fmt.Fprintf(&b, "Prompt: %s\n", r.prompt)
		if r.err != nil {
			fmt.Fprintf(&b, "failed: %s\n", r.err)
			continue
		}
		b.WriteString(r.content)
		b.WriteString("\n")
	}
	return b.String()
}
