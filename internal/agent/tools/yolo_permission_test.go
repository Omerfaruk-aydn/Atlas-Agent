package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
	"github.com/stretchr/testify/require"
)

// runWithin fails the test instead of hanging when a permission prompt
// would wait for an answer nobody gives.
func runWithin(t *testing.T, d time.Duration, fn func(ctx context.Context) fantasy.ToolResponse) fantasy.ToolResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), d)
	defer cancel()
	done := make(chan fantasy.ToolResponse, 1)
	go func() { done <- fn(ctx) }()
	select {
	case resp := <-done:
		return resp
	case <-time.After(d + time.Second):
		t.Fatal("tool call blocked waiting for permission")
		return fantasy.ToolResponse{}
	}
}

func TestYoloBrowserMutationsDoNotWaitForPermission(t *testing.T) {
	t.Parallel()
	perms := permission.NewPermissionService(t.TempDir(), true, nil)
	sessions := newFakeBrowserSessions()
	tool := newBrowserTool(perms, t.TempDir(), sessions, browserDescription(false))
	resp := runWithin(t, 3*time.Second, func(ctx context.Context) fantasy.ToolResponse {
		ctx = context.WithValue(ctx, SessionIDContextKey, "yolo-session")
		resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "click", Name: BrowserToolName, Input: `{"action":"click","selector":"#create"}`})
		require.NoError(t, err)
		return resp
	})
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, "#create", sessions.sessions["yolo-session"].clicked)
}

func TestManualModeBrowserMutationStillAsks(t *testing.T) {
	t.Parallel()
	perms := permission.NewPermissionService(t.TempDir(), false, nil)
	requests := perms.Subscribe(t.Context())
	sessions := newFakeBrowserSessions()
	tool := newBrowserTool(perms, t.TempDir(), sessions, browserDescription(false))
	done := make(chan fantasy.ToolResponse, 1)
	go func() {
		ctx := context.WithValue(t.Context(), SessionIDContextKey, "manual-session")
		resp, _ := tool.Run(ctx, fantasy.ToolCall{ID: "click", Name: BrowserToolName, Input: `{"action":"click","selector":"#delete"}`})
		done <- resp
	}()
	select {
	case event := <-requests:
		require.Equal(t, BrowserToolName, event.Payload.ToolName)
		require.Equal(t, "click", event.Payload.Action)
		select {
		case <-done:
			t.Fatal("the call must wait for the user's decision")
		case <-time.After(50 * time.Millisecond):
		}
		require.True(t, perms.Deny(event.Payload))
	case <-time.After(3 * time.Second):
		t.Fatal("manual mode did not request permission")
	}
	resp := <-done
	require.True(t, resp.IsError)
	require.Nil(t, sessions.sessions["manual-session"], "a denied call must not reach the browser")
}

func TestYoloComputerActionRunsWithoutPrompt(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{}
	perms := permission.NewPermissionService(t.TempDir(), true, nil)
	resp := runWithin(t, 5*time.Second, func(context.Context) fantasy.ToolResponse {
		return runComputerTool(t, backend, perms, true, ComputerParams{Action: "click", X: 100, Y: 200})
	})
	require.False(t, resp.IsError, resp.Content)
	require.Len(t, backend.clicked, 1)
}

func TestYoloPipelineChildrenDoNotWait(t *testing.T) {
	t.Parallel()
	perms := permission.NewPermissionService(t.TempDir(), true, nil)
	sessions := newFakeBrowserSessions()
	browserTool := newBrowserTool(perms, t.TempDir(), sessions, browserDescription(false))
	pipeline := NewToolPipeline(func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return browserTool.Run(ctx, call)
	})
	input, err := json.Marshal(PipelineParams{Steps: []PipelineStep{
		{ID: "open", Tool: BrowserToolName, Arguments: map[string]any{"action": "navigate", "url": "https://example.test/"}},
		{ID: "press", Tool: BrowserToolName, Arguments: map[string]any{"action": "click", "selector": "#continue"}},
	}})
	require.NoError(t, err)
	resp := runWithin(t, 5*time.Second, func(ctx context.Context) fantasy.ToolResponse {
		ctx = context.WithValue(ctx, SessionIDContextKey, "yolo-pipeline")
		resp, err := pipeline.Run(ctx, fantasy.ToolCall{ID: "pipe", Name: "tool_pipeline", Input: string(input)})
		require.NoError(t, err)
		return resp
	})
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, "https://example.test/", sessions.sessions["yolo-pipeline"].navigated)
	require.Equal(t, "#continue", sessions.sessions["yolo-pipeline"].clicked)
}

func TestYoloRefusesConfirmationQuestionsButKeepsInformationRequests(t *testing.T) {
	t.Parallel()
	perms := permission.NewPermissionService(t.TempDir(), true, nil)
	svc := question.NewService()
	tool := NewQuestionTool(svc, perms)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "yolo-question")
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "q1", Input: `{"questions":[{"purpose":"confirmation","type":"yes_no","question":"Devam edeyim mi?","description":"Confirm"}]}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "yolo_confirmation_not_needed")
	require.False(t, resp.StopTurn, "the run continues instead of waiting")

	// Information only the user has is still asked.
	requests := svc.Subscribe(t.Context())
	done := make(chan fantasy.ToolResponse, 1)
	go func() {
		resp, _ := tool.Run(ctx, fantasy.ToolCall{ID: "q2", Input: `{"questions":[{"purpose":"information","type":"free_text","question":"Which Figma team?","description":"Two teams are visible"}]}`})
		done <- resp
	}()
	select {
	case event := <-requests:
		require.True(t, svc.CancelRequest(event.Payload.ID))
	case <-time.After(3 * time.Second):
		t.Fatal("information request was not shown")
	}
	<-done

	// Without YOLO, yes/no confirmations are shown as before.
	perms.SetSkipRequests(false)
	manualDone := make(chan struct{})
	go func() {
		_, _ = tool.Run(ctx, fantasy.ToolCall{ID: "q3", Input: `{"questions":[{"type":"yes_no","question":"Proceed?","description":"Confirm"}]}`})
		close(manualDone)
	}()
	select {
	case event := <-requests:
		require.True(t, svc.CancelRequest(event.Payload.ID))
	case <-time.After(3 * time.Second):
		t.Fatal("manual mode must still show yes/no questions")
	}
	<-manualDone
}

func TestYoloConfirmationPurposeCannotBeChangedByQuestionFormat(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"yes_no", "single_choice", "multi_choice", "free_text"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			perms := permission.NewPermissionService(t.TempDir(), true, nil)
			svc := question.NewService()
			requests := svc.Subscribe(t.Context())
			input, err := json.Marshal(QuestionParams{Questions: []QuestionItem{{
				Purpose: "confirmation", Type: kind, Question: "Devam edeyim mi?", Description: "Confirm continuing the requested work",
				Choices: []QuestionChoice{{ID: "yes", Label: "Evet"}, {ID: "no", Label: "Hayır"}},
			}}})
			require.NoError(t, err)
			resp := runWithin(t, time.Second, func(ctx context.Context) fantasy.ToolResponse {
				resp, err := NewQuestionTool(svc, perms).Run(ctx, fantasy.ToolCall{ID: "confirm", Input: string(input)})
				require.NoError(t, err)
				return resp
			})
			require.True(t, resp.IsError)
			require.False(t, resp.StopTurn)
			require.Contains(t, resp.Content, "yolo_confirmation_not_needed")
			select {
			case <-requests:
				t.Fatal("YOLO confirmation must not open a user prompt")
			default:
			}
		})
	}
}

func TestYoloFactualYesNoQuestionIsAnswered(t *testing.T) {
	t.Parallel()
	perms := permission.NewPermissionService(t.TempDir(), true, nil)
	svc := question.NewService()
	requests := svc.Subscribe(t.Context())
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "yolo-factual")
	done := make(chan fantasy.ToolResponse, 1)
	go func() {
		resp, _ := NewQuestionTool(svc, perms).Run(ctx, fantasy.ToolCall{ID: "fact", Input: `{"questions":[{"purpose":"information","type":"yes_no","question":"Is two-factor authentication enabled?","description":"This account setting cannot be observed here."}]}`})
		done <- resp
	}()
	select {
	case event := <-requests:
		yes := true
		require.True(t, svc.Answer([]question.Answer{{QuestionID: event.Payload.Questions[0].ID, Yes: &yes}}))
	case <-time.After(3 * time.Second):
		t.Fatal("factual yes/no question was blocked in YOLO")
	}
	select {
	case resp := <-done:
		require.False(t, resp.IsError, resp.Content)
		require.Contains(t, resp.Content, "User answered: yes")
	case <-time.After(3 * time.Second):
		t.Fatal("answer was not returned to the model")
	}
}

func TestYoloRejectsAmbiguousAndMixedPurposeBatchesWithoutPrompt(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"missing purpose": `{"questions":[{"type":"single_choice","question":"Continue?","description":"Confirm","choices":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}]}]}`,
		"invalid purpose": `{"questions":[{"purpose":"approval","type":"free_text","question":"Continue?","description":"Confirm"}]}`,
		"mixed batch":     `{"questions":[{"purpose":"information","type":"free_text","question":"Which account?","description":"Two accounts exist"},{"purpose":"confirmation","type":"free_text","question":"Continue?","description":"Confirm"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := question.NewService()
			requests := svc.Subscribe(t.Context())
			perms := permission.NewPermissionService(t.TempDir(), true, nil)
			resp := runWithin(t, time.Second, func(ctx context.Context) fantasy.ToolResponse {
				resp, err := NewQuestionTool(svc, perms).Run(ctx, fantasy.ToolCall{ID: "batch", Input: input})
				require.NoError(t, err)
				return resp
			})
			require.True(t, resp.IsError)
			require.False(t, resp.StopTurn)
			select {
			case <-requests:
				t.Fatal("an invalid batch must not partially open a question")
			default:
			}
		})
	}
}
