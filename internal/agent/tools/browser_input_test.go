package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// fakeInputSession adds the typed input driver to the plain fake session.
type fakeInputSession struct {
	*fakeBrowserSession
	viewport browser.Viewport
	inputs   []browser.Request
	inputErr error
}

func (f *fakeInputSession) ViewportScreenshot(context.Context) ([]byte, browser.Viewport, error) {
	return []byte("png-bytes"), f.viewport, nil
}

func (f *fakeInputSession) Input(_ context.Context, p browser.Request) (browser.InputResult, error) {
	f.inputs = append(f.inputs, p)
	if f.inputErr != nil {
		return browser.InputResult{Action: p.Action}, f.inputErr
	}
	return browser.InputResult{Action: p.Action, EventsSent: 3, ViewportID: p.ViewportID}, nil
}

type fakeInputSessions struct{ sess *fakeInputSession }

func (f *fakeInputSessions) Session(string) (browser.Session, error) { return f.sess, nil }
func (f *fakeInputSessions) Close(string)                            {}

func runInputBrowserTool(t *testing.T, sess *fakeInputSession, input string) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")
	resp, err := newBrowserTool(&mockPermissionService{}, t.TempDir(), &fakeInputSessions{sess}, browserDescription(false)).Run(ctx, fantasy.ToolCall{ID: "input-call", Name: BrowserToolName, Input: input})
	require.NoError(t, err)
	return resp
}

func TestBrowserScreenshotIssuesViewportGrant(t *testing.T) {
	t.Parallel()
	sess := &fakeInputSession{fakeBrowserSession: &fakeBrowserSession{}, viewport: browser.Viewport{ID: "vp-abc", ImageWidth: 1280, ImageHeight: 720, Scale: 1}}
	resp := runInputBrowserTool(t, sess, `{"action":"screenshot"}`)
	require.False(t, resp.IsError)
	require.Equal(t, "image", resp.Type)
	require.Contains(t, resp.Content, `"viewport_id":"vp-abc"`)
	require.Contains(t, resp.Content, "Numbered boxes")
}

func TestBrowserPointerDragReachesDriverWithExactTarget(t *testing.T) {
	t.Parallel()
	sess := &fakeInputSession{fakeBrowserSession: &fakeBrowserSession{}}
	resp := runInputBrowserTool(t, sess, `{"action":"pointer_drag","advanced":{"viewport_id":"vp-abc","x":100,"y":120,"to_x":490,"to_y":964,"modifiers":["shift"],"steps":8}}`)
	require.False(t, resp.IsError, resp.Content)
	require.Len(t, sess.inputs, 1)
	got := sess.inputs[0]
	require.Equal(t, browser.InputPointerDrag, got.Action)
	require.Equal(t, "vp-abc", got.ViewportID)
	require.Equal(t, []float64{100, 120, 490, 964}, []float64{got.X, got.Y, got.ToX, got.ToY})
	require.Equal(t, []string{"shift"}, got.Modifiers)
	require.Equal(t, 8, got.Steps)
}

func TestBrowserInputUsesTopLevelKeyAndText(t *testing.T) {
	t.Parallel()
	sess := &fakeInputSession{fakeBrowserSession: &fakeBrowserSession{}}
	resp := runInputBrowserTool(t, sess, `{"action":"key_combo","key":"a","advanced":{"modifiers":["ctrl"]}}`)
	require.False(t, resp.IsError, resp.Content)
	resp = runInputBrowserTool(t, sess, `{"action":"insert_text","text":"Welcome back"}`)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, "a", sess.inputs[0].Key)
	require.Equal(t, "Welcome back", sess.inputs[1].Text)
}

func TestBrowserInputRejectsInvalidRequestsWithoutSending(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"missing viewport": `{"action":"pointer_click","advanced":{"x":10,"y":10}}`,
		"bad button":       `{"action":"pointer_click","advanced":{"viewport_id":"vp","x":10,"y":10,"button":"thumb"}}`,
		"bad modifier":     `{"action":"key_combo","key":"a","advanced":{"modifiers":["hyper"]}}`,
		"empty text":       `{"action":"insert_text"}`,
		"too many clicks":  `{"action":"pointer_click","advanced":{"viewport_id":"vp","x":10,"y":10,"click_count":5}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sess := &fakeInputSession{fakeBrowserSession: &fakeBrowserSession{}}
			resp := runInputBrowserTool(t, sess, input)
			require.True(t, resp.IsError)
			// Empty insert_text is refused by the shared argument decoder
			// (missing_value); the others by input validation.
			require.True(t, strings.Contains(resp.Content, "invalid_input") || strings.Contains(resp.Content, "missing_value"), resp.Content)
			require.Contains(t, resp.Content, "No input sent")
			require.Empty(t, sess.inputs)
		})
	}
}

func TestBrowserPointerStaleViewportAsksForFreshScreenshot(t *testing.T) {
	t.Parallel()
	sess := &fakeInputSession{fakeBrowserSession: &fakeBrowserSession{}, inputErr: browser.ErrStaleViewport}
	resp := runInputBrowserTool(t, sess, `{"action":"pointer_click","advanced":{"viewport_id":"vp-old","x":10,"y":10}}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "stale_viewport")
	require.Contains(t, resp.Content, "fresh_observation_required")
}

func TestBrowserInputFailureDoesNotInventCleanupSuccess(t *testing.T) {
	t.Parallel()
	for _, inputErr := range []error{errors.New("transport failed"), browser.ErrInputCleanup} {
		sess := &fakeInputSession{fakeBrowserSession: &fakeBrowserSession{}, inputErr: inputErr}
		resp := runInputBrowserTool(t, sess, `{"action":"pointer_drag","advanced":{"viewport_id":"vp","x":10,"y":10,"to_x":20,"to_y":20}}`)
		require.True(t, resp.IsError)
		require.Contains(t, resp.Content, inputErr.Error())
		require.NotContains(t, resp.Content, "held input was released")
	}
}

func TestBrowserWorkflowRunsTypedInputThroughHookedTool(t *testing.T) {
	t.Parallel()
	var calls []BrowserParams
	tool := NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		require.Equal(t, BrowserToolName, call.Name)
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		calls = append(calls, p)
		return fantasy.NewTextResponse(`{"events_sent":3}`), nil
	})
	p := PipelineParams{Browser: &BrowserWorkflowParams{Steps: []BrowserWorkflowStep{
		{ID: "frame", Action: browser.InputPointerDrag, Target: browser.Request{ViewportID: "vp-1", X: 10, Y: 10, ToX: 400, ToY: 800}},
		{ID: "rename", Action: browser.InputKeyCombo, Key: "r", Target: browser.Request{Modifiers: []string{"ctrl"}}},
		{ID: "label", Action: browser.InputInsertText, Text: "Login"},
	}}}
	encoded, err := json.Marshal(p)
	require.NoError(t, err)
	result, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "workflow", Input: string(encoded)})
	require.NoError(t, err)
	require.False(t, result.IsError, result.Content)
	require.Len(t, calls, 3)
	require.Equal(t, browser.InputPointerDrag, calls[0].Action)
	require.Equal(t, "vp-1", calls[0].Advanced.ViewportID)
	require.Equal(t, 800.0, calls[0].Advanced.ToY)
	require.Equal(t, "r", calls[1].Advanced.Key)
	require.Equal(t, "Login", calls[2].Advanced.Text)

	_, err = compileBrowserWorkflow(BrowserWorkflowParams{Steps: []BrowserWorkflowStep{
		{ID: "ok", Action: browser.InputPointerMove, Target: browser.Request{ViewportID: "vp-1"}},
		{ID: "bad", Action: browser.InputPointerClick, Target: browser.Request{X: 3, Y: 4}},
	}})
	require.ErrorContains(t, err, "viewport_id", "a bad later step is rejected before the first input")
}
