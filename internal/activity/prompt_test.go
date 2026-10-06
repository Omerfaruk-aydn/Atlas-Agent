package activity

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func questionFixture(id string) Prompt {
	return Prompt{Kind: KindQuestion, ID: id, Questions: []PromptQuestion{{
		ID: id + "-q", Type: QuestionSingleChoice, Text: "Pick one", Description: "Choose",
		Choices: []PromptChoice{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
	}}}
}

func permissionFixture(id string) Prompt {
	return Prompt{Kind: KindPermission, ID: id, Permission: PromptPermission{
		Tool: "browser", Action: "click",
		Decisions: []PermissionDecision{DecisionAllowOnce, DecisionAllowSession, DecisionDeny},
	}}
}

func pick(id string) PromptResponse {
	return PromptResponse{Answers: []PromptAnswer{{QuestionID: id + "-q", Selected: []string{"b"}}}}
}

func TestQuestionKeepsControlVisibleAndBlocksInput(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(context.Background(), "island-question")
	defer end()
	m.Begin(ctx, Event{Session: "island-question", Resource: "browser"})()
	stop := m.Snapshot()
	require.True(t, stop.CanStop)

	pending := AwaitPrompt(ctx, questionFixture("q1"), func(PromptResponse) bool { return true })
	e := m.Snapshot()
	require.True(t, e.Visible, "Asking must not tear down the control surface")
	require.True(t, e.Persistent)
	require.True(t, e.CanStop, "Stop stays reachable while waiting")
	require.Equal(t, StateAwaitQuestion, e.State)
	require.NotNil(t, e.Prompt)
	require.Equal(t, "q1", e.Prompt.ID)
	require.True(t, PromptBlocks(ctx, false), "The run's new input must wait")

	waited := make(chan error, 1)
	go func() { waited <- WaitForPrompts(ctx, false) }()
	select {
	case <-waited:
		t.Fatal("Input proceeded before the answer")
	case <-time.After(50 * time.Millisecond):
	}
	pending.Done(OutcomeAnswered)
	require.NoError(t, <-waited)
	e = m.Snapshot()
	require.True(t, e.Visible)
	require.Nil(t, e.Prompt)
	require.Equal(t, StateResuming, e.State)
	require.Equal(t, stop.RunStarted, e.RunStarted, "Waiting must not restart the timer")
}

func TestPermissionDecisionIsDeliveredOnceToTheDisplayedRequest(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(context.Background(), "island-permission")
	defer end()
	m.Begin(ctx, Event{Session: "island-permission"})()

	var calls atomic.Int32
	var got PermissionDecision
	resolved := make(chan struct{})
	pending := AwaitPrompt(ctx, permissionFixture("p1"), func(r PromptResponse) bool {
		calls.Add(1)
		got = r.Decision
		close(resolved)
		return true
	})
	revision := m.Snapshot().Prompt.Revision
	require.Equal(t, StateAwaitPermission, m.Snapshot().State)

	require.ErrorIs(t, m.Respond(revision+1, PromptResponse{Decision: DecisionAllowOnce}), ErrPromptStale)
	require.ErrorIs(t, m.Respond(revision, PromptResponse{Decision: DecisionNone}), ErrPromptInvalid)
	require.Zero(t, calls.Load(), "Rejected responses must not reach the service")

	require.NoError(t, m.Respond(revision, PromptResponse{Decision: DecisionDeny}))
	<-resolved
	require.Equal(t, StateDelivering, m.Snapshot().State)
	require.ErrorIs(t, m.Respond(revision, PromptResponse{Decision: DecisionAllowOnce}), ErrPromptBusy, "A double click must not deliver twice")
	pending.Done(OutcomeDenied)
	require.ErrorIs(t, m.Respond(revision, PromptResponse{Decision: DecisionAllowOnce}), ErrPromptStale)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, DecisionDeny, got)
	require.Equal(t, StateDenied, m.Snapshot().State, "A refusal never reads as continuing")
}

func TestPromptsQueueWithoutDroppingAndAnswerByRevision(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(context.Background(), "island-queue")
	defer end()
	m.Begin(ctx, Event{Session: "island-queue"})()

	answered := map[string]int{}
	respond := func(id string) PromptResponder {
		return func(PromptResponse) bool { answered[id]++; return true }
	}
	first := AwaitPrompt(ctx, questionFixture("first"), respond("first"))
	second := AwaitPrompt(ctx, permissionFixture("second"), respond("second"))
	e := m.Snapshot()
	require.Equal(t, "first", e.Prompt.ID)
	require.Equal(t, 1, e.Prompt.Queued)
	firstRevision := e.Prompt.Revision

	// The terminal answers the first request: the island moves on and the
	// old revision can no longer answer anything.
	first.Done(OutcomeAnswered)
	e = m.Snapshot()
	require.Equal(t, "second", e.Prompt.ID)
	require.Zero(t, e.Prompt.Queued)
	require.Equal(t, StateAwaitPermission, e.State)
	require.ErrorIs(t, m.Respond(firstRevision, pick("first")), ErrPromptStale)
	require.ErrorIs(t, m.Respond(firstRevision, PromptResponse{Decision: DecisionAllowOnce}), ErrPromptStale)
	require.NoError(t, m.Respond(e.Prompt.Revision, PromptResponse{Decision: DecisionAllowOnce}))
	second.Done(OutcomeGranted)
	require.Equal(t, map[string]int{"second": 1}, answered)
	require.Nil(t, m.Snapshot().Prompt)
}

func TestQuestionResponseIsValidatedAgainstItsRequest(t *testing.T) {
	p := questionFixture("v")
	p.Questions = append(p.Questions,
		PromptQuestion{ID: "yes", Type: QuestionYesNo},
		PromptQuestion{ID: "multi", Type: QuestionMultiChoice, Choices: []PromptChoice{{ID: "x"}, {ID: "y"}}},
		PromptQuestion{ID: "text", Type: QuestionFreeText},
	)
	yes := true
	valid := PromptResponse{Answers: []PromptAnswer{
		{QuestionID: "v-q", Selected: []string{"a"}},
		{QuestionID: "yes", Yes: &yes},
		{QuestionID: "multi", Selected: []string{"x", "y"}},
		{QuestionID: "text", Text: "Merhaba dünya — çğıöşü 你好"},
	}}
	require.True(t, p.accepts(valid))
	for name, mutate := range map[string]func(*PromptResponse){
		"two picks for single": func(r *PromptResponse) { r.Answers[0].Selected = []string{"a", "b"} },
		"unknown choice":       func(r *PromptResponse) { r.Answers[0].Selected = []string{"z"} },
		"wrong question":       func(r *PromptResponse) { r.Answers[1].QuestionID = "other" },
		"missing answer":       func(r *PromptResponse) { r.Answers = r.Answers[:3] },
		"duplicate pick":       func(r *PromptResponse) { r.Answers[2].Selected = []string{"x", "x"} },
		"oversized text":       func(r *PromptResponse) { r.Answers[3].Text = string(make([]rune, MaxPromptText+1)) },
		"decision on question": func(r *PromptResponse) { r.Decision = DecisionAllowOnce },
	} {
		t.Run(name, func(t *testing.T) {
			r := valid
			r.Answers = append([]PromptAnswer(nil), valid.Answers...)
			mutate(&r)
			require.False(t, p.accepts(r))
		})
	}
}

func TestCancelWhileWaitingTearsDownAndRejectsOldAnswers(t *testing.T) {
	for _, during := range []string{"question", "permission"} {
		t.Run(during, func(t *testing.T) {
			m := New(&testRenderer{})
			defer m.Close()
			ctx, end := StartFlow(context.Background(), "island-cancel")
			m.Begin(ctx, Event{Session: "island-cancel"})()
			p := questionFixture("c")
			if during == "permission" {
				p = permissionFixture("c")
			}
			var calls atomic.Int32
			pending := AwaitPrompt(ctx, p, func(PromptResponse) bool { calls.Add(1); return true })
			revision := m.Snapshot().Prompt.Revision
			CancelSession("island-cancel")
			e := m.Snapshot()
			require.False(t, e.Visible)
			require.Nil(t, e.Prompt)
			require.Equal(t, StateCancelled, e.State)
			require.False(t, AwaitingUser(ctx), "A stopped run has nothing pending")
			require.ErrorIs(t, m.Respond(revision, PromptResponse{Decision: DecisionAllowOnce}), ErrPromptStale)
			require.Zero(t, calls.Load(), "A cancelled request is never approved")
			pending.Done(OutcomeCancelled)
			require.False(t, m.Snapshot().Visible, "A late resolution must not revive the surface")
			end()

			next, endNext := StartFlow(context.Background(), "island-cancel")
			defer endNext()
			m.Begin(next, Event{Session: "island-cancel"})()
			e = m.Snapshot()
			require.True(t, e.Visible, "A new task starts cleanly")
			require.Nil(t, e.Prompt)
			require.Equal(t, StateThinking, e.State)
		})
	}
}

func TestPromptOfAnotherSessionLeavesOtherSurfacesAlone(t *testing.T) {
	a := New(&testRenderer{})
	defer a.Close()
	b := New(&testRenderer{})
	defer b.Close()
	ctxA, endA := StartFlow(context.Background(), "session-a")
	defer endA()
	ctxB, endB := StartFlow(context.Background(), "session-b")
	defer endB()
	a.Begin(ctxA, Event{Session: "session-a"})()
	b.Begin(ctxB, Event{Session: "session-b"})()
	pending := AwaitPrompt(ctxA, questionFixture("a"), func(PromptResponse) bool { return true })
	require.Nil(t, b.Snapshot().Prompt)
	require.Equal(t, StateThinking, b.Snapshot().State)
	require.False(t, PromptBlocks(ctxB, false), "Another run's browser input is independent")
	require.True(t, PromptBlocks(ctxB, true), "The shared desktop waits for the displayed question")
	CancelSession("session-a")
	require.True(t, b.Snapshot().Visible, "Cancelling one run keeps the other's surface")
	require.False(t, PromptBlocks(ctxB, true))
	pending.Done(OutcomeCancelled)
}
