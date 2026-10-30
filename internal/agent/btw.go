package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

// A side question ("/btw") is a question asked while the main work goes
// on. It is answered by a separate, read-only agent in a scratch session
// that is deleted again afterwards, so nothing about it reaches the main
// conversation: the main session's history, its system prompt and its
// tool list are never touched, which also keeps the main prompt cache
// intact. The main agent may be busy while this runs; the two share
// nothing but the project on disk.

const (
	// sideQuestionRole names the model role that may route side
	// questions to a model of their own (options.model_roles.btw).
	// Without one they run on the session's large model.
	sideQuestionRole = "btw"

	// SideQuestionMaxLength bounds a question, in bytes. A side question
	// is a sentence or two; anything longer belongs in the main session.
	SideQuestionMaxLength = 4000

	// sideQuestionContextBudget bounds the excerpt of the main
	// conversation handed to the side agent, in bytes. Recent turns are
	// kept, older ones dropped first.
	sideQuestionContextBudget = 12000

	// sideQuestionMessageBudget bounds one message inside the excerpt.
	sideQuestionMessageBudget = 1500

	// sideQuestionTimeout ends a side question that never finishes. The
	// caller's own cancellation still applies and ends it sooner.
	sideQuestionTimeout = 5 * time.Minute
)

// sideQuestionTools is everything a side question may use: reading code
// and history, never writing, running commands or reaching the network.
// buildSubagentSessionAgent narrows it further to what the primary agent
// has, so a tool the user disabled stays disabled here.
var sideQuestionTools = []string{
	"view", "glob", "grep", "ls",
	"lsp_symbols", "lsp_definition", "lsp_call_hierarchy", "lsp_references",
	"git_status", "git_diff", "git_log", "git_blame",
	"import_graph", "project_map",
}

const sideQuestionInstructions = `You are answering a quick side question that the user asked while their main agent keeps working on something else.

- Answer the question directly and concisely. Lead with the answer.
- You are read-only. Inspect files with the tools you have when the answer depends on the code; do not guess about files you could read. Never claim to have changed anything.
- The conversation excerpt is background only, and the main agent may still be changing files, so what you read can move under you.
- This exchange is not part of the main conversation. Do not ask follow-up questions and do not offer to take actions; if the question needs a change, say what the change would be.
- Do not plan or take on the main task. Answer only what was asked.`

// SideAnswer is the outcome of a side question.
type SideAnswer struct {
	// Text is the answer, as markdown.
	Text string
	// Model is the model that produced it, for display.
	Model string
}

// sideQuestionSubagent describes the throwaway agent that answers.
func sideQuestionSubagent() *subagents.Subagent {
	return &subagents.Subagent{
		Name:         sideQuestionRole,
		Description:  "Answers a side question without touching the main conversation.",
		ReadOnly:     true,
		Tools:        sideQuestionTools,
		Instructions: sideQuestionInstructions,
	}
}

// AskSideQuestion answers question in a separate read-only context. The
// session is only a source of background and of the place to bill cost
// to: nothing is appended to it. It may be empty (no conversation yet), in
// which case the agent gets no excerpt.
func (c *coordinator) AskSideQuestion(ctx context.Context, sessionID, question string) (SideAnswer, error) {
	question, err := normalizeSideQuestion(question)
	if err != nil {
		return SideAnswer{}, err
	}

	agentCfg, ok := c.cfg.Config().Agents[config.AgentTask]
	if !ok {
		return SideAnswer{}, errors.New("task agent not configured")
	}
	agent, err := c.buildSubagentSessionAgent(ctx, agentCfg, sideQuestionSubagent())
	if err != nil {
		return SideAnswer{}, fmt.Errorf("prepare side question: %w", err)
	}
	return c.runSideQuestion(ctx, agent, sessionID, question)
}

// runSideQuestion runs question on agent inside a scratch session that is
// removed again whatever the outcome.
func (c *coordinator) runSideQuestion(ctx context.Context, agent SessionAgent, parentSessionID, question string) (SideAnswer, error) {
	ctx, cancel := context.WithTimeout(ctx, sideQuestionTimeout)
	defer cancel()

	var scratch string
	if parentSessionID != "" {
		child, err := c.sessions.CreateChild(ctx, parentSessionID, "Side question")
		if err != nil {
			return SideAnswer{}, fmt.Errorf("create side question session: %w", err)
		}
		scratch = child.ID
	} else {
		// No conversation yet, so there is nothing to hang the scratch
		// session off.
		created, err := c.sessions.Create(ctx, "Side question")
		if err != nil {
			return SideAnswer{}, fmt.Errorf("create side question session: %w", err)
		}
		scratch = created.ID
	}
	defer func() {
		// The caller may have cancelled; the scratch session still has
		// to go.
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer done()
		if parentSessionID != "" {
			if err := c.updateParentSessionCostDelta(cleanup, scratch, parentSessionID, 0); err != nil {
				slog.Warn("Failed to bill side question cost to its session", "session", parentSessionID, "error", err)
			}
		}
		if err := c.sessions.Delete(cleanup, scratch); err != nil {
			slog.Warn("Failed to delete side question session", "session", scratch, "error", err)
		}
	}()

	prompt := question
	if parentSessionID != "" {
		msgs, err := c.messages.List(ctx, parentSessionID)
		if err != nil {
			slog.Warn("Failed to read the conversation for a side question", "session", parentSessionID, "error", err)
		} else {
			prompt = buildSideQuestionPrompt(sideQuestionExcerpt(msgs, sideQuestionContextBudget), question)
		}
	}

	model := agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}
	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return SideAnswer{}, errModelProviderNotConfigured
	}

	result, err := agent.Run(ctx, SessionAgentCall{
		SessionID:        scratch,
		Prompt:           prompt,
		MaxOutputTokens:  maxTokens,
		ProviderOptions:  getProviderOptions(model, providerCfg),
		Temperature:      model.ModelCfg.Temperature,
		TopP:             model.ModelCfg.TopP,
		TopK:             model.ModelCfg.TopK,
		FrequencyPenalty: model.ModelCfg.FrequencyPenalty,
		PresencePenalty:  model.ModelCfg.PresencePenalty,
		NonInteractive:   true,
		OnAuthRefresh:    c.makeAuthRefreshCallback(providerCfg),
	})
	if err != nil {
		return SideAnswer{}, fmt.Errorf("side question failed: %w", err)
	}
	text := strings.TrimSpace(subAgentOutput(result))
	if text == "" {
		return SideAnswer{}, errors.New("the side question produced no answer")
	}
	return SideAnswer{Text: text, Model: model.ModelCfg.Model}, nil
}

// normalizeSideQuestion trims question and rejects one that is empty or
// too long to be a side question.
func normalizeSideQuestion(question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", errors.New("a side question needs a question")
	}
	if len(question) > SideQuestionMaxLength {
		return "", fmt.Errorf("a side question is limited to %d characters; ask it in the main session instead", SideQuestionMaxLength)
	}
	return question, nil
}

// buildSideQuestionPrompt joins the conversation excerpt and the question
// into the one user message the side agent receives.
func buildSideQuestionPrompt(excerpt, question string) string {
	if excerpt == "" {
		return question
	}
	return "<main_conversation_excerpt>\n" + excerpt + "\n</main_conversation_excerpt>\n\n<side_question>\n" + question + "\n</side_question>"
}

// sideQuestionExcerpt renders the most recent part of a conversation as
// plain text within budget bytes. Tool results and reasoning are left out:
// they are bulk, and the side agent can read the files themselves.
func sideQuestionExcerpt(msgs []message.Message, budget int) string {
	var lines []string
	used := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		line := sideQuestionLine(&msgs[i])
		if line == "" {
			continue
		}
		if used+len(line) > budget && len(lines) > 0 {
			break
		}
		lines = append(lines, line)
		used += len(line) + 1
	}
	for l, r := 0, len(lines)-1; l < r; l, r = l+1, r-1 {
		lines[l], lines[r] = lines[r], lines[l]
	}
	return strings.Join(lines, "\n")
}

// sideQuestionLine renders one message, or "" when it has nothing worth
// showing.
func sideQuestionLine(m *message.Message) string {
	var who string
	switch m.Role {
	case message.User:
		who = "User"
	case message.Assistant:
		who = "Assistant"
	default:
		return ""
	}
	if m.IsSummaryMessage {
		who = "Summary of earlier work"
	}

	text := strings.TrimSpace(m.Content().Text)
	if m.Role == message.Assistant {
		var names []string
		for _, call := range m.ToolCalls() {
			names = append(names, call.Name)
		}
		if len(names) > 0 {
			note := "[used tools: " + strings.Join(names, ", ") + "]"
			if text == "" {
				text = note
			} else {
				text += " " + note
			}
		}
	}
	if text == "" {
		return ""
	}
	if len(text) > sideQuestionMessageBudget {
		cut := sideQuestionMessageBudget
		for cut > 0 && !isRuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + " [truncated]"
	}
	return who + ": " + text
}

// isRuneStart reports whether b begins a UTF-8 sequence rather than
// continuing one.
func isRuneStart(b byte) bool {
	return b&0xC0 != 0x80
}
