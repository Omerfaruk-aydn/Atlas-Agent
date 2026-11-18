package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func verdictSchema(t *testing.T) *resultSchema {
	t.Helper()
	s, err := compileResultSchema(map[string]any{
		"type":     "object",
		"required": []any{"verdict", "files"},
		"properties": map[string]any{
			"verdict": map[string]any{"type": "string", "enum": []any{"pass", "fail"}},
			"files":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, s)
	return s
}

func TestCompileResultSchemaRejectsBadSchemas(t *testing.T) {
	s, err := compileResultSchema(nil)
	require.NoError(t, err)
	require.Nil(t, s, "no schema means free text, as before")

	_, err = compileResultSchema(map[string]any{"type": 12})
	require.ErrorContains(t, err, "result_schema")

	big := map[string]any{"description": strings.Repeat("x", maxResultSchemaBytes)}
	_, err = compileResultSchema(big)
	require.ErrorContains(t, err, "keep it under")
}

func TestResultSchemaAcceptsWellFormedRepliesInAnyWrapping(t *testing.T) {
	s := verdictSchema(t)
	want := `{"files":["a.go"],"verdict":"pass"}`
	for name, reply := range map[string]string{
		"bare":          `{"verdict":"pass","files":["a.go"]}`,
		"padded":        "\n  {\"verdict\": \"pass\", \"files\": [\"a.go\"]}\n",
		"json fence":    "```json\n{\"verdict\":\"pass\",\"files\":[\"a.go\"]}\n```",
		"plain fence":   "```\n{\"verdict\":\"pass\",\"files\":[\"a.go\"]}\n```",
		"prose around":  "Here is my answer: {\"verdict\":\"pass\",\"files\":[\"a.go\"]} Hope that helps.",
		"fence + prose": "Done.\n```json\n{\"verdict\":\"pass\",\"files\":[\"a.go\"]}\n```\nBye.",
	} {
		clean, problems := s.check(reply)
		require.Empty(t, problems, name)
		require.JSONEq(t, want, clean, name)
		require.NotContains(t, clean, " ", "%s: the parent gets compact JSON", name)
	}
}

func TestResultSchemaNamesWhatIsWrong(t *testing.T) {
	s := verdictSchema(t)
	for name, tc := range map[string]struct{ reply, want string }{
		"no json":        {"I reviewed everything and it looks fine.", "no JSON"},
		"broken json":    {`{"verdict": "pass",`, "no JSON"},
		"missing field":  {`{"verdict":"pass"}`, "files"},
		"wrong enum":     {`{"verdict":"maybe","files":[]}`, "/verdict"},
		"wrong item":     {`{"verdict":"pass","files":[1]}`, "should be string"},
		"not an object":  {`["pass"]`, "/"},
		"empty":          {"", "no JSON"},
		"only a fence":   {"```json\n```", "no JSON"},
		"text then junk": {"result: {oops}", "no JSON"},
	} {
		clean, problems := s.check(tc.reply)
		require.Empty(t, clean, name)
		require.Contains(t, problems, tc.want, name)
	}
}

func TestRetryPromptCarriesProblemsAndSchema(t *testing.T) {
	s := verdictSchema(t)
	_, problems := s.check(`{"verdict":"maybe","files":[]}`)
	prompt := s.retryPrompt(problems)
	require.Contains(t, prompt, problems)
	require.Contains(t, prompt, `"required"`)
	require.Contains(t, prompt, "only the corrected JSON")
	require.Contains(t, s.instruction(), `"verdict"`)
}

// The whole loop through runSubAgent: a reply that misses the schema goes back
// to the same agent with the problems, and the parent receives only JSON.
func TestRunSubAgentReturnsValidatedJSONAfterACorrection(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})
	parent, err := env.sessions.Create(t.Context(), "Parent")
	require.NoError(t, err)

	var prompts []string
	mock := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		prompts = append(prompts, call.Prompt)
		if len(prompts) == 1 {
			return agentResultWithText("All good, it passes."), nil
		}
		return agentResultWithText("```json\n{\"verdict\": \"pass\", \"files\": [\"a.go\"]}\n```"), nil
	})

	resp, err := coord.runSubAgent(t.Context(), subAgentParams{
		Agent: mock, SessionID: parent.ID, AgentMessageID: "m1", ToolCallID: "c1",
		Prompt: "review it", SessionTitle: "t", ResultSchema: verdictSchema(t),
	})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.JSONEq(t, `{"verdict":"pass","files":["a.go"]}`, resp.Content)
	require.NotContains(t, resp.Content, "\n", "no run note around the JSON")

	require.Len(t, prompts, 2)
	require.Contains(t, prompts[0], "review it")
	require.Contains(t, prompts[0], `"required"`, "the schema is shown up front")
	require.Contains(t, prompts[1], "did not match the required result schema")
	require.Contains(t, prompts[1], "no JSON")
}

func TestRunSubAgentGivesUpAfterTheRetryBudget(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})
	parent, err := env.sessions.Create(t.Context(), "Parent")
	require.NoError(t, err)

	calls := 0
	mock := newMockAgent(providerID, 4096, func(_ context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
		calls++
		return agentResultWithText("still prose"), nil
	})
	resp, err := coord.runSubAgent(t.Context(), subAgentParams{
		Agent: mock, SessionID: parent.ID, AgentMessageID: "m1", ToolCallID: "c1",
		Prompt: "review it", SessionTitle: "t", ResultSchema: verdictSchema(t),
	})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "did not match result_schema")
	require.Contains(t, resp.Content, "still prose", "the last reply is shown so the work is not lost")
	require.Equal(t, maxResultSchemaRetries+1, calls)
}

func TestRunSubAgentWithoutASchemaIsUnchanged(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})
	parent, err := env.sessions.Create(t.Context(), "Parent")
	require.NoError(t, err)

	mock := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		require.Equal(t, "plain task", call.Prompt, "no schema text is added")
		return agentResultWithText("free text answer"), nil
	})
	resp, err := coord.runSubAgent(t.Context(), subAgentParams{
		Agent: mock, SessionID: parent.ID, AgentMessageID: "m1", ToolCallID: "c1", Prompt: "plain task", SessionTitle: "t",
	})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "free text answer")
}
