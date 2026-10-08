package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func runPreflightBrowser(t *testing.T, sess *fakeBrowserSession, input string) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "preflight-session")
	resp, err := newBrowserTool(&mockPermissionService{}, t.TempDir(), &fakeBrowserSessions{sessions: map[string]*fakeBrowserSession{"preflight-session": sess}}, browserDescription(false)).Run(ctx, fantasy.ToolCall{ID: "c", Name: BrowserToolName, Input: input})
	require.NoError(t, err)
	return resp
}

// TestBrowserTextSelectorIsDiagnosedNotSent replays the call from session
// aaae8f0a: a Playwright text= selector reached querySelectorAll and failed
// with a SyntaxError. It must be refused before any click, naming the
// supported semantic route.
func TestBrowserTextSelectorIsDiagnosedNotSent(t *testing.T) {
	t.Parallel()
	sess := &fakeBrowserSession{}
	resp := runPreflightBrowser(t, sess, `{"action":"click","selector":"text=06 · Oturumlar"}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "unsupported_selector")
	require.Contains(t, resp.Content, "semantic_click")
	require.Contains(t, resp.Content, `\"name\":\"06 · Oturumlar\"`)
	require.Empty(t, sess.clicked, "nothing may be clicked")

	for _, selector := range []string{"role=button[name=Share]", `button:has-text("Share")`, "//div[@id='a']", "#a >> text=b"} {
		require.Error(t, browserArgumentPreflight("click", BrowserParams{Selector: selector}, false), selector)
	}
	for _, selector := range []string{"#frame-06", `[aria-label="Right sidebar"] input`, "div:nth-child(2)", "a[href^='https']"} {
		require.NoError(t, browserArgumentPreflight("click", BrowserParams{Selector: selector}, false), selector)
	}
}

// TestBrowserTypeWithoutTargetSendsNothing replays the session's type call
// that carried only advanced.role/name: it used to fail as driver_error
// with dispatch unknown, which made the pipeline mark it uncertain.
func TestBrowserTypeWithoutTargetSendsNothing(t *testing.T) {
	t.Parallel()
	sess := &fakeBrowserSession{}
	resp := runPreflightBrowser(t, sess, `{"action":"type","advanced":{"role":"combobox","name":"Font size: 14 px"},"text":"28"}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "missing_target")
	require.Contains(t, resp.Content, "set_field")
	require.Empty(t, sess.typedText)

	resp = runPreflightBrowser(t, sess, `{"action":"type","text":"hello"}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "insert_text")
	require.Empty(t, sess.typedText)

	code, _ := browserErrorCode(resp.Content)
	require.Equal(t, "missing_target", code)
	require.Equal(t, "none", dispatchState("type", resp, code))
}

// TestPipelinePreflightStopsBeforeFirstStep: a later step that cannot run
// as written is reported before the earlier steps change anything.
func TestPipelinePreflightStopsBeforeFirstStep(t *testing.T) {
	t.Parallel()
	calls := 0
	tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.NewTextResponse("ok"), nil
	})
	for _, input := range []string{
		`{"steps":[{"id":"font","tool":"browser","arguments":{"action":"key","key":"t"}},{"id":"type","tool":"browser","arguments":{"action":"type","advanced":{"role":"combobox","name":"Font size"},"text":"28"}}]}`,
		`{"browser":{"steps":[{"id":"esc","action":"key","key":"escape"},{"id":"sel","action":"click","target":{"selector":"text=06 · Oturumlar"}}]}}`,
	} {
		resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "p", Name: "tool_pipeline", Input: input})
		require.NoError(t, err)
		require.True(t, resp.IsError, resp.Content)
		require.Contains(t, resp.Content, "No step of this pipeline was run")
	}
	require.Zero(t, calls)

	// A ref supplied by a binding satisfies the target at run time.
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "p", Name: "tool_pipeline", Input: `{"steps":[{"id":"f","tool":"browser","arguments":{"action":"find","advanced":{"name":"Font size"}}},{"id":"t","tool":"browser","bindings":{"ref":"f#/matches/0/ref"},"arguments":{"action":"type","text":"28"}}]}`})
	require.NoError(t, err)
	require.NotContains(t, resp.Content, "No step of this pipeline was run")
}

// TestPipelineFailureNamesResumePoint: after a partial success the failing
// step says which steps already took effect and what is left to send.
func TestPipelineFailureNamesResumePoint(t *testing.T) {
	t.Parallel()
	var sent []string
	tool := NewToolPipeline(func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		sent = append(sent, p.Action+":"+p.Ref)
		if p.Ref == "e3709" {
			return fantasy.NewTextErrorResponse(`click failed: ref "e3709" is gone from the page` + "\n[op] " + `{"dispatch":"none","error_code":"target_missing"}`), nil
		}
		return fantasy.NewTextResponse(`{"status":"applied"}`), nil
	})
	input := `{"steps":[{"id":"w","tool":"browser","arguments":{"action":"set_field","ref":"e1","advanced":{"value":"390"}}},{"id":"sel","tool":"browser","arguments":{"action":"click","ref":"e3709"}},{"id":"edit","tool":"browser","arguments":{"action":"key","key":"enter"}},{"id":"text","tool":"browser","arguments":{"action":"insert_text","text":"x"}}]}`
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "p", Name: "tool_pipeline", Input: input})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, []string{"set_field:e1", "click:e3709"}, sent, "dependent steps after the failure must not run")
	var results []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &results))
	var resume string
	for _, r := range results {
		if r.ID == "sel" {
			resume = r.Resume
		}
	}
	require.Contains(t, resume, "Already applied, do not repeat: w")
	require.Contains(t, resume, "Not run: edit, text")
}

// focusInputSession reports a configurable focus for insert_text.
type focusInputSession struct {
	*fakeInputSession
	focus    browser.FocusInfo
	focusErr error
}

func (f *focusInputSession) Focus(context.Context) (browser.FocusInfo, error) {
	return f.focus, f.focusErr
}

type focusInputSessions struct{ sess *focusInputSession }

func (f *focusInputSessions) Session(string) (browser.Session, error) { return f.sess, nil }
func (f *focusInputSessions) Close(string)                            {}

// TestInsertTextRequiresEditableFocus: text is never inserted where focus
// is unknown or not editable (the body, a button), because editors read
// stray keys as shortcuts.
func TestInsertTextRequiresEditableFocus(t *testing.T) {
	t.Parallel()
	run := func(sess *focusInputSession) fantasy.ToolResponse {
		ctx := context.WithValue(t.Context(), SessionIDContextKey, "focus-session")
		resp, err := newBrowserTool(&mockPermissionService{}, t.TempDir(), &focusInputSessions{sess}, browserDescription(false)).Run(ctx, fantasy.ToolCall{ID: "c", Name: BrowserToolName, Input: `{"action":"insert_text","text":"Merhaba"}`})
		require.NoError(t, err)
		return resp
	}
	body := &focusInputSession{fakeInputSession: &fakeInputSession{fakeBrowserSession: &fakeBrowserSession{}}, focus: browser.FocusInfo{Tag: "body", Kind: "none"}}
	resp := run(body)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "focus_not_editable")
	require.Empty(t, body.inputs)

	unknown := &focusInputSession{fakeInputSession: &fakeInputSession{fakeBrowserSession: &fakeBrowserSession{}}, focusErr: errors.New("target closed")}
	resp = run(unknown)
	require.True(t, resp.IsError)
	require.Empty(t, unknown.inputs)

	editor := &focusInputSession{fakeInputSession: &fakeInputSession{fakeBrowserSession: &fakeBrowserSession{}}, focus: browser.FocusInfo{Tag: "div", Kind: "contenteditable", Editable: true}}
	resp = run(editor)
	require.False(t, resp.IsError, resp.Content)
	require.Len(t, editor.inputs, 1)
	require.Contains(t, resp.Content, `"kind":"contenteditable"`)
}
