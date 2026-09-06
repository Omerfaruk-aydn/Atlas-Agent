package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

// Asking several models the same question independently -- which is what
// orchestrate does -- tells you whether they agree. It does not tell you
// who is right when they do not, because none of them ever had to answer
// the others' reasoning.
//
// A debate does. Round one is orchestrate: everyone answers cold, with no
// chance to anchor on anybody else. Every round after that hands each
// agent what the others said and asks them to defend, concede or change
// their mind. What comes out is either a position that survived being
// argued against, or a disagreement that is now specific enough to be
// worth something.
//
// Disagreement is reported rather than resolved by fiat. Two agents still
// holding opposite views after arguing is a finding about the question --
// flattening it into a consensus nobody reached would be worse than not
// having asked.

//go:embed templates/debate_tool.md
var debateToolDescription string

const DebateToolName = "debate"

// debateMinAgents is two because an argument needs someone to argue with.
const debateMinAgents = 2

// debateMaxAgents caps the panel. Past about five the extra agent mostly
// restates one already there, at the full price of another model call
// every round.
const debateMaxAgents = 5

// debateDefaultRounds is one round to take positions and one to answer
// each other, which is where nearly all of the value is.
const debateDefaultRounds = 2

// debateMaxRounds bounds the cost: agents × rounds model calls, and past
// this they converge on wording rather than substance.
const debateMaxRounds = 4

type DebateParams struct {
	Question   string     `json:"question" description:"What is being decided, with the context needed to decide it. Each agent sees only this, never the caller's conversation."`
	AgentNames stringList `json:"agent_names" description:"Two to five distinct subagent names to put the question to. Pick ones that will disagree."`
	Rounds     int        `json:"rounds,omitempty" description:"How many times they see each other's positions and respond. Default 2, maximum 4."`
	JudgeAgent string     `json:"judge_agent,omitempty" description:"A subagent, distinct from agent_names, that reads the whole exchange and writes the conclusion. Optional."`
}

type DebateResponseMetadata struct {
	Agents []string `json:"agents"`
	Rounds int      `json:"rounds"`
	Judge  string   `json:"judge,omitempty"`
}

// debateTool runs a multi-round argument between named subagents. It
// shares its runner, limiter and subagent resolution with orchestrate;
// the difference is that every round after the first is prompted with
// what the others said.
func (c *coordinator) debateTool(ctx context.Context) (fantasy.AgentTool, error) {
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
	limiter := newConcurrencyLimiter(opts.MaxConcurrentSubAgents)

	return fantasy.NewAgentTool(
		DebateToolName,
		debateToolDescription+describeConfiguredSubagents(discovered),
		func(ctx context.Context, params DebateParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			question := strings.TrimSpace(params.Question)
			if question == "" {
				return fantasy.NewTextErrorResponse("question is required"), nil
			}

			names, err := uniqueDebateAgents(params.AgentNames)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			judgeName := strings.TrimSpace(params.JudgeAgent)
			if judgeName != "" && slices.Contains(names, judgeName) {
				return fantasy.NewTextErrorResponse(
					"judge_agent must be a different agent from agent_names -- it has to weigh the argument, not be a side in it"), nil
			}

			rounds := params.Rounds
			if rounds <= 0 {
				rounds = debateDefaultRounds
			}
			rounds = min(rounds, debateMaxRounds)

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}
			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, errors.New("agent message id missing from context")
			}

			run := func(name, prompt, toolCallID string) orchestrateResult {
				return c.runOrchestratedAgent(ctx, orchestratedAgentParams{
					agentCfg:          agentCfg,
					discovered:        discovered,
					subagentInstances: subagentInstances,
					limiter:           limiter,
					name:              name,
					sessionID:         sessionID,
					agentMessageID:    agentMessageID,
					toolCallID:        toolCallID,
					prompt:            prompt,
				})
			}

			var results []orchestrateResult
			for round := 1; round <= rounds; round++ {
				prompts := make([]string, len(names))
				for i, name := range names {
					if round == 1 {
						// Nobody sees anybody in the opening round, so
						// the first position each agent takes is its
						// own rather than a reaction to whoever
						// answered fastest.
						prompts[i] = buildDebateOpeningPrompt(question)
						continue
					}
					prompts[i] = buildDebateRebuttalPrompt(question, name, results, round, rounds)
				}

				next := make([]orchestrateResult, len(names))
				var wg sync.WaitGroup
				for i, name := range names {
					wg.Add(1)
					go func(i int, name string) {
						defer wg.Done()
						next[i] = run(name, prompts[i], fmt.Sprintf("%s-r%d-%s", call.ID, round, name))
					}(i, name)
				}
				wg.Wait()
				results = next

				// An argument nobody is left to make is not worth
				// paying for another round of.
				if countDebateAnswers(results) < debateMinAgents {
					break
				}
			}

			var judge *orchestrateResult
			if judgeName != "" {
				res := run(judgeName, buildDebateJudgePrompt(question, results), fmt.Sprintf("%s-judge-%s", call.ID, judgeName))
				judge = &res
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(formatDebateResults(question, rounds, results, judge)),
				DebateResponseMetadata{Agents: names, Rounds: rounds, Judge: judgeName},
			), nil
		},
	), nil
}

// uniqueDebateAgents validates the panel: distinct names, enough of them
// to disagree, few enough to stay affordable.
func uniqueDebateAgents(raw []string) ([]string, error) {
	var names []string
	for _, n := range raw {
		n = strings.TrimSpace(n)
		if n == "" || slices.Contains(names, n) {
			continue
		}
		names = append(names, n)
	}
	if len(names) < debateMinAgents {
		return nil, fmt.Errorf("agent_names needs at least %d distinct agents -- an argument needs someone to argue with", debateMinAgents)
	}
	if len(names) > debateMaxAgents {
		return nil, fmt.Errorf("agent_names takes at most %d agents; past that the extra one mostly restates another at the price of a model call every round", debateMaxAgents)
	}
	return names, nil
}

func countDebateAnswers(results []orchestrateResult) int {
	n := 0
	for _, r := range results {
		if r.err == nil && strings.TrimSpace(r.content) != "" {
			n++
		}
	}
	return n
}

func buildDebateOpeningPrompt(question string) string {
	return "Several agents are being asked this question independently, and will then read each other's answers and respond.\n\n" +
		"Give your answer and the reasoning behind it. Be specific and commit to a position -- \"it depends\" helps nobody, " +
		"and hedging now only means being argued with later about nothing. Say plainly what you are unsure of.\n\n" +
		"QUESTION:\n" + question
}

// buildDebateRebuttalPrompt shows one agent what everybody said,
// including itself, and asks it to answer them.
func buildDebateRebuttalPrompt(question, self string, results []orchestrateResult, round, rounds int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This is round %d of %d in a debate between agents on the question below. Here is what each of you said last round.\n\n", round, rounds)
	b.WriteString("Answer the others. Where they are right and you were wrong, say so and change your position -- conceding a point you " +
		"cannot defend is the useful outcome here, not a loss. Where they are wrong, say exactly where and why, not that you disagree. " +
		"Do not restate your previous answer unedited, and do not soften it into agreement you do not hold.\n\n")
	b.WriteString("End with your position as it now stands.\n\n")
	fmt.Fprintf(&b, "QUESTION:\n%s\n\nLAST ROUND:\n", question)
	for _, r := range results {
		label := r.name
		if r.name == self {
			label = r.name + " (you)"
		}
		fmt.Fprintf(&b, "\n=== %s ===\n", label)
		if r.err != nil {
			fmt.Fprintf(&b, "(did not answer: %s)\n", r.err)
			continue
		}
		b.WriteString(r.content)
		b.WriteString("\n")
	}
	return b.String()
}

func buildDebateJudgePrompt(question string, results []orchestrateResult) string {
	var b strings.Builder
	b.WriteString("Several agents have debated the question below and these are their final positions. " +
		"Write the conclusion.\n\n" +
		"Say what the answer is and why that answer rather than the alternatives. Where they still disagree, " +
		"say so and say what the disagreement actually turns on -- do not average two positions into one nobody holds, " +
		"and do not present a contested answer as settled. If they share a flaw, say that instead of picking between them.\n\n")
	fmt.Fprintf(&b, "QUESTION:\n%s\n\nFINAL POSITIONS:\n", question)
	for _, r := range results {
		fmt.Fprintf(&b, "\n=== %s ===\n", r.name)
		if r.err != nil {
			fmt.Fprintf(&b, "(did not answer: %s)\n", r.err)
			continue
		}
		b.WriteString(r.content)
		b.WriteString("\n")
	}
	return b.String()
}

// formatDebateResults renders what came back. The judge goes first when
// there is one, because it is the answer; the positions stay underneath
// it so the caller can see what it was drawn from rather than taking it
// on trust.
func formatDebateResults(question string, rounds int, results []orchestrateResult, judge *orchestrateResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Debate on: %s\n(%d agents, %d rounds)\n", question, len(results), rounds)

	if judge != nil {
		b.WriteString("\n=== conclusion")
		fmt.Fprintf(&b, " (%s) ===\n", judge.name)
		if judge.err != nil {
			fmt.Fprintf(&b, "(the judge failed: %s -- the positions below are unjudged)\n", judge.err)
		} else {
			b.WriteString(strings.TrimSpace(judge.content))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n=== final positions ===\n")
	for _, r := range results {
		fmt.Fprintf(&b, "\n--- %s ---\n", r.name)
		if r.err != nil {
			fmt.Fprintf(&b, "(did not answer: %s)\n", r.err)
			continue
		}
		b.WriteString(strings.TrimSpace(r.content))
		b.WriteString("\n")
	}
	return b.String()
}
