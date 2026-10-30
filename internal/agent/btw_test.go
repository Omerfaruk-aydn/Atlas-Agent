package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSideQuestion(t *testing.T) {
	t.Parallel()

	got, err := normalizeSideQuestion("  what does Foo do?\n")
	require.NoError(t, err)
	require.Equal(t, "what does Foo do?", got)

	_, err = normalizeSideQuestion("   ")
	require.Error(t, err)

	_, err = normalizeSideQuestion(strings.Repeat("x", SideQuestionMaxLength+1))
	require.Error(t, err)
}

func TestSideQuestionToolsAreReadOnly(t *testing.T) {
	t.Parallel()

	for _, name := range sideQuestionTools {
		require.Contains(t, subagents.ReadTools, name)
		require.NotContains(t, subagents.CommandTools, name)
	}
	for _, spawner := range subagentSpawningTools {
		require.NotContains(t, sideQuestionTools, spawner)
	}

	// Whatever the primary agent can do, the side agent keeps only the
	// read-only part of it.
	coder := append([]string{"bash", "edit", "write", "agent", "multiedit"}, sideQuestionTools...)
	allowed := subagentAllowedTools(sideQuestionSubagent(), coder)
	require.ElementsMatch(t, sideQuestionTools, allowed)
}

func TestSideQuestionExcerptKeepsRecentMessagesWithinBudget(t *testing.T) {
	t.Parallel()

	user := func(text string) message.Message {
		return message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}
	}
	assistant := func(text string, tools ...string) message.Message {
		parts := []message.ContentPart{message.TextContent{Text: text}}
		for _, name := range tools {
			parts = append(parts, message.ToolCall{Name: name})
		}
		return message.Message{Role: message.Assistant, Parts: parts}
	}
	msgs := []message.Message{
		user("first question"),
		assistant("first answer", "grep"),
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{Content: "bulk"}}},
		user("second question"),
		assistant("", "view", "edit"),
	}

	all := sideQuestionExcerpt(msgs, 10_000)
	require.Equal(t, "User: first question\nAssistant: first answer [used tools: grep]\nUser: second question\nAssistant: [used tools: view, edit]", all)

	// A tight budget drops the oldest messages first and always keeps
	// the newest one.
	tight := sideQuestionExcerpt(msgs, 50)
	require.Equal(t, "Assistant: [used tools: view, edit]", tight)

	require.Empty(t, sideQuestionExcerpt(nil, 100))
}

func TestSideQuestionLineTruncatesOnRuneBoundary(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", sideQuestionMessageBudget)
	line := sideQuestionLine(&message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: long}}})
	require.True(t, strings.HasSuffix(line, "[truncated]"))
	require.Equal(t, line, strings.ToValidUTF8(line, ""), "truncation must not split a rune")
}

func TestBuildSideQuestionPrompt(t *testing.T) {
	t.Parallel()

	require.Equal(t, "why?", buildSideQuestionPrompt("", "why?"))
	got := buildSideQuestionPrompt("User: hi", "why?")
	require.Contains(t, got, "<main_conversation_excerpt>\nUser: hi\n</main_conversation_excerpt>")
	require.Contains(t, got, "<side_question>\nwhy?\n</side_question>")
}

func TestRunSideQuestionLeavesTheSessionUntouched(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})

	parent, err := env.sessions.Create(t.Context(), "Main")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), parent.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "refactor the parser"}},
	})
	require.NoError(t, err)

	var scratchID, seenPrompt string
	mock := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		scratchID, seenPrompt = call.SessionID, call.Prompt
		require.NotEqual(t, parent.ID, call.SessionID, "the main session must not be the one that runs")
		require.True(t, call.NonInteractive)
		return agentResultWithText("  It parses tokens.  "), nil
	})

	answer, err := coord.runSideQuestion(t.Context(), mock, parent.ID, "what does the parser do?")
	require.NoError(t, err)
	require.Equal(t, "It parses tokens.", answer.Text)

	require.Contains(t, seenPrompt, "User: refactor the parser")
	require.Contains(t, seenPrompt, "what does the parser do?")

	// Nothing was appended to the main session, and the scratch
	// session is gone.
	msgs, err := env.messages.List(t.Context(), parent.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	_, err = env.sessions.Get(t.Context(), scratchID)
	require.Error(t, err)
	children, err := env.sessions.ListByParent(t.Context(), parent.ID)
	require.NoError(t, err)
	require.Empty(t, children)
}

func TestRunSideQuestionWithoutSession(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})

	var scratchID, seenPrompt string
	mock := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		scratchID, seenPrompt = call.SessionID, call.Prompt
		return agentResultWithText("answer"), nil
	})

	answer, err := coord.runSideQuestion(t.Context(), mock, "", "hello?")
	require.NoError(t, err)
	require.Equal(t, "answer", answer.Text)
	require.Equal(t, "hello?", seenPrompt)
	_, err = env.sessions.Get(t.Context(), scratchID)
	require.Error(t, err, "the scratch session must be deleted")
}

func TestRunSideQuestionFailureStillCleansUp(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})

	parent, err := env.sessions.Create(t.Context(), "Main")
	require.NoError(t, err)

	var scratchID string
	mock := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		scratchID = call.SessionID
		return nil, errors.New("boom")
	})

	_, err = coord.runSideQuestion(t.Context(), mock, parent.ID, "q")
	require.ErrorContains(t, err, "boom")
	_, err = env.sessions.Get(t.Context(), scratchID)
	require.Error(t, err)

	empty := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return agentResultWithText("   "), nil
	})
	_, err = coord.runSideQuestion(t.Context(), empty, parent.ID, "q")
	require.Error(t, err)
}

func TestAskSideQuestionRejectsEmptyQuestion(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})

	_, err := coord.AskSideQuestion(t.Context(), "", "  ")
	require.Error(t, err)
}
