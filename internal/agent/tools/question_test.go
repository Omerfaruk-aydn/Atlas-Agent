package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
	"github.com/stretchr/testify/require"
)

type activityQuestionService struct {
	question.Service
	ask func(context.Context, question.Request) ([]question.Answer, error)
}

func (s activityQuestionService) Ask(ctx context.Context, req question.Request) ([]question.Answer, error) {
	return s.ask(ctx, req)
}

func TestQuestionKeepsControlAndIsAnsweredFromTheIsland(t *testing.T) {
	for _, resource := range []string{"desktop", "browser"} {
		t.Run(resource, func(t *testing.T) {
			manager := activity.New(pointerTestRenderer{})
			defer manager.Close()
			ctx, end := activity.StartFlow(t.Context(), "question-owner")
			defer end()
			ctx = engineering.WithScope(ctx, "question-owner", "worker")
			ctx = context.WithValue(ctx, SessionIDContextKey, "worker-session")
			manager.Begin(ctx, activity.Event{Session: "question-owner", Resource: resource})()
			stop := manager.StopRevision()
			require.NotZero(t, stop)

			svc := question.NewService()
			island := make(chan error, 1)
			go func() {
				require.Eventually(t, func() bool { return manager.Snapshot().Prompt != nil }, 2*time.Second, time.Millisecond)
				e := manager.Snapshot()
				require.True(t, e.Visible, "Control stays on screen while the user answers")
				require.Equal(t, activity.StateAwaitQuestion, e.State)
				require.Equal(t, stop, manager.StopRevision(), "Stop keeps working while waiting")
				require.True(t, activity.AwaitingUser(ctx), "Run input waits for the answer")
				island <- manager.Respond(e.Prompt.Revision, activity.PromptResponse{Answers: []activity.PromptAnswer{{QuestionID: e.Prompt.Questions[0].ID, Text: "devam et — çğış"}}})
			}()
			resp, err := NewQuestionTool(svc).Run(ctx, fantasy.ToolCall{ID: "question-call", Input: `{"questions":[{"type":"free_text","question":"Continue?","description":"Choose the next step"}]}`})
			require.NoError(t, err)
			require.NoError(t, <-island)
			require.False(t, resp.IsError)
			require.Contains(t, resp.Content, "devam et — çğış")
			e := manager.Snapshot()
			require.True(t, e.Visible, "The same run continues on the same surface")
			require.Nil(t, e.Prompt)
			require.Equal(t, activity.StateResuming, e.State)
			require.False(t, activity.AwaitingUser(ctx))
		})
	}
}

func TestQuestionAnsweredInTerminalCannotBeAnsweredAgainFromIsland(t *testing.T) {
	manager := activity.New(pointerTestRenderer{})
	defer manager.Close()
	ctx, end := activity.StartFlow(t.Context(), "terminal-owner")
	defer end()
	ctx = context.WithValue(ctx, SessionIDContextKey, "terminal-owner")
	manager.Begin(ctx, activity.Event{Session: "terminal-owner"})()
	svc := question.NewService()
	requests := svc.Subscribe(t.Context())
	done := make(chan fantasy.ToolResponse, 1)
	go func() {
		resp, _ := NewQuestionTool(svc).Run(ctx, fantasy.ToolCall{ID: "q", Input: `{"questions":[{"type":"yes_no","question":"Proceed?","description":"Confirm"}]}`})
		done <- resp
	}()
	req := (<-requests).Payload
	require.Eventually(t, func() bool { return manager.Snapshot().Prompt != nil }, 2*time.Second, time.Millisecond)
	revision := manager.Snapshot().Prompt.Revision
	yes := true
	require.True(t, svc.Answer([]question.Answer{{QuestionID: req.Questions[0].ID, Yes: &yes}}))
	resp := <-done
	require.Contains(t, resp.Content, "yes")
	no := false
	err := manager.Respond(revision, activity.PromptResponse{Answers: []activity.PromptAnswer{{QuestionID: req.Questions[0].ID, Yes: &no}}})
	require.ErrorIs(t, err, activity.ErrPromptStale)
	require.Nil(t, manager.Snapshot().Prompt, "The terminal answer collapses the island")
}

func TestQuestionParamsUnmarshalJSON_NativeArray(t *testing.T) {
	t.Parallel()
	input := `{"questions": [{"type": "yes_no", "question": "OK?", "description": "test"}]}`
	var p QuestionParams
	require.NoError(t, json.Unmarshal([]byte(input), &p))
	require.Len(t, p.Questions, 1)
	require.Equal(t, "OK?", p.Questions[0].Question)
}

func TestQuestionParamsUnmarshalJSON_StringEncodedArray(t *testing.T) {
	t.Parallel()
	// Simulates a model that double-serializes the questions field.
	inner := `[{"type":"yes_no","question":"OK?","description":"test"}]`
	encoded, _ := json.Marshal(inner)
	input := `{"questions": ` + string(encoded) + `}`
	var p QuestionParams
	require.NoError(t, json.Unmarshal([]byte(input), &p))
	require.Len(t, p.Questions, 1)
	require.Equal(t, "OK?", p.Questions[0].Question)
}

func TestQuestionParamsUnmarshalJSON_StringEncodedWithWhitespace(t *testing.T) {
	t.Parallel()
	inner := `  [{"type":"single_choice","question":"Pick","description":"d","choices":[{"id":"a","label":"A"}]}]  `
	encoded, _ := json.Marshal(inner)
	input := `{"questions": ` + string(encoded) + `, "confirm_title": "Go?"}`
	var p QuestionParams
	require.NoError(t, json.Unmarshal([]byte(input), &p))
	require.Len(t, p.Questions, 1)
	require.Equal(t, "Pick", p.Questions[0].Question)
	require.Equal(t, "Go?", p.ConfirmTitle)
}

func TestQuestionParamsUnmarshalJSON_InvalidString(t *testing.T) {
	t.Parallel()
	encoded, _ := json.Marshal("not valid json")
	input := `{"questions": ` + string(encoded) + `}`
	var p QuestionParams
	require.Error(t, json.Unmarshal([]byte(input), &p))
}

func TestFormatAnswer_MultiChoiceWithFillIn(t *testing.T) {
	answer := question.Answer{
		SelectedIDs: []string{"speed", "readability"},
		FillInText:  "maintainability",
	}
	resp, err := formatAnswer(&answer, question.TypeMultiChoice)
	require.NoError(t, err)
	require.Contains(t, resp.Content, `User selected: ["speed","readability"]`)
	require.Contains(t, resp.Content, "User provided: maintainability")
}

func TestFormatAnswer_SelectionsOnly(t *testing.T) {
	answer := question.Answer{SelectedIDs: []string{"gardening"}}
	resp, err := formatAnswer(&answer, question.TypeSingleChoice)
	require.NoError(t, err)
	require.Contains(t, resp.Content, `User selected: ["gardening"]`)
	require.NotContains(t, resp.Content, "User provided")
}

func TestFormatAnswer_Skipped(t *testing.T) {
	answer := question.Answer{}
	resp, err := formatAnswer(&answer, question.TypeFreeText)
	require.NoError(t, err)
	require.Equal(t, "User skipped this question", resp.Content)
}
