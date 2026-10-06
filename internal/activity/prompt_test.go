package activity

import (
	"context"

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
