package agent

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

func TestParseEvaluatorVerdict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		reply    string
		pass     bool
		findings string
		ok       bool
	}{
		{name: "pass", reply: "VERDICT: PASS", pass: true, ok: true},
		{name: "pass after preamble", reply: "I checked the diff.\nverdict: pass", pass: true, ok: true},
		{name: "fail with findings", reply: "VERDICT: FAIL\n- a.go:3 nil map write\n- b.go:9 error ignored", findings: "- a.go:3 nil map write\n- b.go:9 error ignored", ok: true},
		{name: "fail without findings", reply: "VERDICT: FAIL", pass: true, ok: true},
		{name: "unknown verdict", reply: "VERDICT: MAYBE"},
		{name: "no verdict", reply: "Looks fine to me."},
		{name: "empty", reply: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pass, findings, ok := parseEvaluatorVerdict(tc.reply)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.pass, pass)
			require.Equal(t, tc.findings, findings)
		})
	}
}

func TestEvaluatorLedgerRecordsChangesAndCommands(t *testing.T) {
	t.Parallel()

	step := fantasy.StepResult{Response: fantasy.Response{Content: fantasy.ResponseContent{
		fantasy.ToolCallContent{ToolCallID: "1", ToolName: "edit", Input: `{"file_path":"a.go"}`},
		fantasy.ToolResultContent{ToolCallID: "1", ToolName: "edit", Result: fantasy.ToolResultOutputContentText{Text: "ok"}},
		fantasy.ToolCallContent{ToolCallID: "2", ToolName: "write", Input: `{"file_path":"b.go"}`},
		fantasy.ToolResultContent{ToolCallID: "2", ToolName: "write", Result: fantasy.ToolResultOutputContentError{Error: context.Canceled}},
		fantasy.ToolCallContent{ToolCallID: "3", ToolName: "bash", Input: `{"command":"go test ./..."}`},
		fantasy.ToolResultContent{ToolCallID: "3", ToolName: "bash", Result: fantasy.ToolResultOutputContentError{Error: context.DeadlineExceeded}},
		fantasy.ToolCallContent{ToolCallID: "4", ToolName: "edit", Input: `{"file_path":"a.go"}`},
		fantasy.ToolResultContent{ToolCallID: "4", ToolName: "edit", Result: fantasy.ToolResultOutputContentText{Text: "ok"}},
	}}}

	var l evaluatorLedger
	l.record([]fantasy.StepResult{step})

	require.True(t, l.changed)
	require.Equal(t, []string{"a.go"}, l.files, "failed writes and duplicates are not recorded")
	require.Len(t, l.commands, 1)
	require.Equal(t, "go test ./...", l.commands[0].command)
	require.True(t, l.commands[0].failed)

	review := l.review("All tests pass.")
	require.Contains(t, review, "- a.go")
	require.Contains(t, review, "[failed] go test ./...")
	require.Contains(t, review, "All tests pass.")
}

type editInput struct {
	FilePath string `json:"file_path"`
}

func fakeEditTool() fantasy.AgentTool {
	return fantasy.NewAgentTool("edit", "Edit a file.", func(ctx context.Context, p editInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("edited " + p.FilePath), nil
	})
}

func withEvaluator(a *sessionAgent, model fantasy.LanguageModel, rounds int) {
	cat := catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}
	a.SetEvaluatorOptions(&Model{Model: model, CatwalkCfg: cat}, nil, rounds)
}

// A failing verdict becomes the agent's next instruction, and the second
// review sees the first one's findings.
func TestEvaluatorSendsFailingTurnBack(t *testing.T) {
	t.Parallel()

	model := &scriptedModel{steps: []scriptedStep{
		{tool: "edit", input: `{"file_path":"a.go"}`},
		{text: "Done."},
		{tool: "edit", input: `{"file_path":"a.go"}`},
		{text: "Fixed the nil check."},
	}}
	evaluator := &scriptedModel{steps: []scriptedStep{
		{text: "VERDICT: FAIL\n- a.go:3 nil map write"},
		{text: "VERDICT: PASS"},
	}}
	a, env, _ := newWorkLoopAgent(t, model, fakeEditTool())
	withEvaluator(a, evaluator, 2)
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)

	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "add the cache", MaxOutputTokens: 1000})
	require.NoError(t, err)

	calls := model.calls()
	require.Len(t, calls, 4)
	require.Contains(t, requestText(calls[2]), "did not accept it")
	require.Contains(t, requestText(calls[2]), "a.go:3 nil map write")

	reviews := evaluator.calls()
	require.Len(t, reviews, 2)
	require.Contains(t, requestText(reviews[0]), "add the cache")
	require.Contains(t, requestText(reviews[1]), "previous verdict")
}

// Once the round limit is spent the turn ends and says what is unresolved.
func TestEvaluatorStopsAtRoundLimit(t *testing.T) {
	t.Parallel()

	model := &scriptedModel{steps: []scriptedStep{
		{tool: "edit", input: `{"file_path":"a.go"}`},
		{text: "Done."},
		{tool: "edit", input: `{"file_path":"a.go"}`},
		{text: "Done again."},
	}}
	evaluator := &scriptedModel{repeat: &scriptedStep{text: "VERDICT: FAIL\n- a.go:3 still broken"}}
	a, env, _ := newWorkLoopAgent(t, model, fakeEditTool())
	withEvaluator(a, evaluator, 1)
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)

	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "add the cache", MaxOutputTokens: 1000})
	require.NoError(t, err)

	require.Len(t, model.calls(), 4)
	require.Len(t, evaluator.calls(), 2)

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	var last *message.Message
	for i := range msgs {
		if msgs[i].Role == message.Assistant {
			last = &msgs[i]
		}
	}
	require.NotNil(t, last)
	finish := last.FinishPart()
	require.NotNil(t, finish)
	require.Equal(t, "Evaluator findings unresolved", finish.Message)
	require.Contains(t, finish.Details, "still broken")
}

// A turn that changed no files is not reviewed.
func TestEvaluatorSkipsTurnWithoutChanges(t *testing.T) {
	t.Parallel()

	model := &scriptedModel{steps: []scriptedStep{{text: "The cache lives in cache.go."}}}
	evaluator := &scriptedModel{repeat: &scriptedStep{text: "VERDICT: FAIL\n- unused"}}
	a, env, _ := newWorkLoopAgent(t, model)
	withEvaluator(a, evaluator, 2)
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)

	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "where is the cache?", MaxOutputTokens: 1000})
	require.NoError(t, err)

	require.Len(t, model.calls(), 1)
	require.Empty(t, evaluator.calls())
}
