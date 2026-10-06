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
