package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

// scriptedModel answers each request with the next scripted step: a tool
// call when call is set, otherwise text. It keeps every request it saw.
type scriptedModel struct {
	mu       sync.Mutex
	steps    []scriptedStep
	repeat   *scriptedStep
	requests []fantasy.Call
}

type scriptedStep struct {
	tool, input, text string
}

func (m *scriptedModel) Provider() string { return "fake" }
func (m *scriptedModel) Model() string    { return "fake-model" }

func (m *scriptedModel) next(call fantasy.Call) scriptedStep {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, call)
	if len(m.steps) == 0 {
		if m.repeat != nil {
			s := *m.repeat
			// Vary the input so only the failure class repeats.
			s.input = strings.ReplaceAll(s.input, "{n}", fmt.Sprint(len(m.requests)))
			return s
		}
		return scriptedStep{text: "done"}
	}
	s := m.steps[0]
	m.steps = m.steps[1:]
	return s
}

func (m *scriptedModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not used")
}

func (m *scriptedModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	s := m.next(call)
	n := len(m.requests)
	return func(yield func(fantasy.StreamPart) bool) {
		if s.tool == "" {
			for _, p := range []fantasy.StreamPart{{Type: fantasy.StreamPartTypeTextStart, ID: "t"}, {Type: fantasy.StreamPartTypeTextDelta, ID: "t", Delta: s.text}, {Type: fantasy.StreamPartTypeTextEnd, ID: "t"}} {
				if !yield(p) {
					return
				}
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
			return
		}
		id := fmt.Sprintf("call-%d", n)
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: id, ToolCallName: s.tool, ToolCallInput: s.input}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls})
	}, nil
}

func (m *scriptedModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not used")
}

func (m *scriptedModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not used")
}

func (m *scriptedModel) calls() []fantasy.Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]fantasy.Call(nil), m.requests...)
}

type probeInput struct {
	Port int `json:"port"`
}

// failingProbe fails the same way whatever port it is given.
func failingProbe() fantasy.AgentTool {
	return fantasy.NewAgentTool("probe", "Connect to a port.", func(ctx context.Context, p probeInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("dial tcp 127.0.0.1:%d: connect: connection refused", p.Port)), nil
	})
}

func newWorkLoopAgent(t *testing.T, model fantasy.LanguageModel, extra ...fantasy.AgentTool) (*sessionAgent, fakeEnv, *engineering.Store) {
	t.Helper()
	env := testEnv(t)
	store := engineering.NewStore(t.TempDir())
	cat := catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}
	a := NewSessionAgent(SessionAgentOptions{
		LargeModel:   Model{Model: model, CatwalkCfg: cat},
		SmallModel:   Model{Model: &finishStreamModel{text: "title"}, CatwalkCfg: cat},
		SystemPrompt: "system",
		IsYolo:       true,
		Sessions:     env.sessions,
		Messages:     env.messages,
		Tools:        extra,
		WorkingDir:   env.workingDir,
		Engineering:  store,
	}).(*sessionAgent)
	return a, env, store
}

func requestText(call fantasy.Call) string {
	var b strings.Builder
	for _, msg := range call.Prompt {
		for _, part := range msg.Content {
			if text, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok {
				b.WriteString(text.Text)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// A rephrased retry of the same failure gets one strategy review, recorded
// in the work state, before the model is asked again.
func TestStrategyReviewAfterRephrasedRetries(t *testing.T) {
	t.Parallel()

	model := &scriptedModel{steps: []scriptedStep{
		{tool: "probe", input: `{"port":5432}`},
		{tool: "probe", input: `{"port":5433}`},
		{tool: "probe", input: `{"port":6543}`},
		{text: "The database is not listening; I will check how it is started."},
	}}
	a, env, store := newWorkLoopAgent(t, model, failingProbe())
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)

	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "connect the service to its database and fix the connection", MaxOutputTokens: 1000})
	require.NoError(t, err)

	calls := model.calls()
	require.Len(t, calls, 4)
	for i := range 3 {
		require.NotContains(t, requestText(calls[i]), "<strategy_review>")
	}
	review := requestText(calls[3])
	require.Contains(t, review, "<strategy_review>")
	require.Contains(t, review, "rephrased_retry")
	require.Contains(t, review, "connection refused")

	st, err := store.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, 1, st.Work.Strategy.Signals)
	require.Len(t, st.Work.Attempts, 1)
	require.Equal(t, "strategy_monitor", st.Work.Attempts[0].Source)
}

// When reviews do not change the approach the turn ends, bounded, with a
// stated reason, well before the generic loop guard.
func TestStrategyMonitorStopsPersistentFailure(t *testing.T) {
	t.Parallel()

	// Inputs differ every time, so the identical-call circuit breaker never
	// fires; only the repeated failure class is visible.
	model := &scriptedModel{repeat: &scriptedStep{tool: "probe", input: `{"port":54{n}}`}}
	a, env, store := newWorkLoopAgent(t, model, failingProbe())
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)

	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "fix the database connection", MaxOutputTokens: 1000})
	require.NoError(t, err)
	require.Equal(t, 5, len(model.calls()), "two reviews, then the third signal stops the turn")

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	last := msgs[len(msgs)-1]
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == message.Assistant {
			last = msgs[i]
			break
		}
	}
	finish := last.FinishPart()
	require.NotNil(t, finish)
	require.Equal(t, "Stopped without progress", finish.Message)
	require.Contains(t, finish.Details, "persisted after 2 strategy reviews")

	st, err := store.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "waiting", st.Work.Phase)
	require.Contains(t, st.Work.Waiting, "different approach or user input")
}

// The work state is shown every turn, and the budget follows the request.
func TestWorkStateReachesThePromptAndFollowsTheRequest(t *testing.T) {
	t.Parallel()

	model := &scriptedModel{}
	a, env, store := newWorkLoopAgent(t, model)
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)

	run := func(prompt string) fantasy.Call {
		before := len(model.calls())
		_, err := a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: prompt, MaxOutputTokens: 1000})
		require.NoError(t, err)
		return model.calls()[before]
	}
	first := run("What does the summarize threshold control?")
	require.Contains(t, first.Prompt[0].Content[0].(fantasy.TextPart).Text, `"tier":"direct"`)
	require.Contains(t, first.Prompt[0].Content[0].(fantasy.TextPart).Text, "project_context")

	run("Refactor the cache architecture so eviction is pluggable")
	st, err := store.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, engineering.TierDeep, st.Work.Budget.Tier)
	require.Len(t, st.Work.Turns, 2)

	third := run("devam et")
	st, err = store.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "continuation", st.Work.Turns[2].Kind)
	require.Equal(t, engineering.TierDeep, st.Work.Budget.Tier)
	require.Contains(t, third.Prompt[0].Content[0].(fantasy.TextPart).Text, "<work_state>")
}

// The workflow work action records contract items and refuses unobserved
// verification; verification binds to an actual observed check.
func TestWorkflowWorkActionBindsObservedEvidence(t *testing.T) {
	t.Parallel()

	store := engineering.NewStore(t.TempDir())
	c := &coordinator{engineering: store}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")
	_, err := recordWorkTurn(ctx, store, "s", "Make the parser accept empty input", "")
	require.NoError(t, err)

	call := func(action string, in WorkInput) fantasy.ToolResponse {
		resp, err := c.workWorkflow(ctx, WorkflowParams{Action: "work", WorkAction: action, Work: &in}, fantasy.ToolCall{ID: "w"})
		require.NoError(t, err)
		return resp
	}
	resp := call("contract_add", WorkInput{Kind: "behavior", Text: "parser accepts empty input", Origin: "user"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "nothing was recorded")
	resp = call("contract_add", WorkInput{Kind: "behavior", Text: "parser accepts empty input", Origin: "user", SourceTurns: []int{1}})
	require.False(t, resp.IsError, resp.Content)

	resp = call("contract_advance", WorkInput{ItemID: "R1", Status: "verified"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "no passing check")

	op, err := store.Begin(t.Context(), "s", "test-call", "test_run", `{}`, "")
	require.NoError(t, err)
	require.NoError(t, store.FinishObserved(t.Context(), "s", op, true, false, engineering.Hash("ok"), true))

	resp = call("contract_advance", WorkInput{ItemID: "R1", Status: "verified"})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, op)

	resp = call("status", WorkInput{})
	require.Contains(t, resp.Content, `"call_id":"test-call"`)

	resp = call("unknown", WorkInput{})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "contract_add")
}
