package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// fakeBrowserSession is a browser.Session double so these tests exercise
// the tool's dispatch/validation/permission logic without launching a real
// browser.
type fakeBrowserSession struct {
	navigated   string
	wentBack    bool
	wentForward bool
	clicked     string
	typedSel    string
	typedText   string
	pressedKey  string
	scrolledDX  int
	scrolledDY  int
	evalScript  string
	evalErr     error
	textSel     string
	htmlSel     string
	screenshot  []byte
	url         string

	snapshot     []browser.SnapshotElement
	snapshotErr  error
	snapshotFull bool
	images       []browser.ImageInfo
	imagesErr    error
	console      []browser.ConsoleEntry
	dialogs      []browser.DialogInfo
	dialogAccept *bool
	dialogText   string
	dialogErr    error
	cdpMethod    string
	cdpParams    map[string]any
	cdpResult    map[string]any
	cdpErr       error
}

func (f *fakeBrowserSession) Navigate(url string) error { f.navigated = url; return nil }
func (f *fakeBrowserSession) Back() error               { f.wentBack = true; return nil }
func (f *fakeBrowserSession) Forward() error            { f.wentForward = true; return nil }
func (f *fakeBrowserSession) Click(selector string) error {
	f.clicked = selector
	return nil
}

func (f *fakeBrowserSession) Type(selector, text string) error {
	f.typedSel, f.typedText = selector, text
	return nil
}

func (f *fakeBrowserSession) PressKey(name string) error {
	if _, ok := browser.ResolveKey(name); !ok {
		return fmt.Errorf("unsupported key %q", name)
	}
	f.pressedKey = name
	return nil
}

func (f *fakeBrowserSession) Scroll(dx, dy int) error {
	f.scrolledDX, f.scrolledDY = dx, dy
	return nil
}

func (f *fakeBrowserSession) Eval(script string) (string, error) {
	f.evalScript = script
	if f.evalErr != nil {
		return "", f.evalErr
	}
	return "42", nil
}

func (f *fakeBrowserSession) Text(selector string) (string, error) {
	f.textSel = selector
	return "hello world", nil
}

func (f *fakeBrowserSession) HTML(selector string) (string, error) {
	f.htmlSel = selector
	return "<p>hi</p>", nil
}

func (f *fakeBrowserSession) Screenshot(bool) ([]byte, error) {
	return []byte("png-bytes"), nil
}

func (f *fakeBrowserSession) Snapshot(full bool) ([]browser.SnapshotElement, error) {
	f.snapshotFull = full
	return f.snapshot, f.snapshotErr
}

func (f *fakeBrowserSession) Images() ([]browser.ImageInfo, error) {
	return f.images, f.imagesErr
}

func (f *fakeBrowserSession) ConsoleLogs() []browser.ConsoleEntry {
	return f.console
}

func (f *fakeBrowserSession) PendingDialogs() []browser.DialogInfo {
	return f.dialogs
}

func (f *fakeBrowserSession) HandleDialog(accept bool, promptText string) error {
	f.dialogAccept, f.dialogText = &accept, promptText
	return f.dialogErr
}

func (f *fakeBrowserSession) RawCDP(method string, params map[string]any) (map[string]any, error) {
	f.cdpMethod, f.cdpParams = method, params
	return f.cdpResult, f.cdpErr
}

func (f *fakeBrowserSession) URL() (string, error) { return f.url, nil }
func (f *fakeBrowserSession) Close()               {}

// fakeBrowserSessions is the browserSessions double: one session per ID,
// created lazily, tracking Close calls.
type fakeBrowserSessions struct {
	sessions  map[string]*fakeBrowserSession
	closed    []string
	launchErr error
}

func newFakeBrowserSessions() *fakeBrowserSessions {
	return &fakeBrowserSessions{sessions: map[string]*fakeBrowserSession{}}
}

func (f *fakeBrowserSessions) Session(id string) (browser.Session, error) {
	if f.launchErr != nil {
		return nil, f.launchErr
	}
	s, ok := f.sessions[id]
	if !ok {
		s = &fakeBrowserSession{}
		f.sessions[id] = s
	}
	return s, nil
}

func (f *fakeBrowserSessions) Close(id string) {
	f.closed = append(f.closed, id)
	delete(f.sessions, id)
}

func runBrowserTool(t *testing.T, sessions *fakeBrowserSessions, perms permission.Service, params BrowserParams) fantasy.ToolResponse {
	t.Helper()

	input, err := json.Marshal(params)
	require.NoError(t, err)

	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")
	resp, err := newBrowserTool(perms, t.TempDir(), sessions, browserDescription(false)).Run(ctx, fantasy.ToolCall{
		ID:    "test-call",
		Name:  BrowserToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	return resp
}

func TestBrowserToolNavigateRequiresURL(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "navigate"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "url is required")
}

func TestBrowserToolNavigateRejectsNonHTTPScheme(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "navigate", URL: "file:///etc/passwd"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "http")
}

func TestBrowserToolNavigateDrivesTheSession(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "navigate", URL: "https://example.com"})
	require.False(t, resp.IsError)
	require.Equal(t, "https://example.com", sessions.sessions["test-session"].navigated)
}

func TestBrowserToolClickRequiresSelector(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "click"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "selector is required")
}

func TestBrowserToolTypeReplacesFieldContents(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "type", Selector: "#q", Text: "hello"})
	require.False(t, resp.IsError)
	require.Equal(t, "#q", sessions.sessions["test-session"].typedSel)
	require.Equal(t, "hello", sessions.sessions["test-session"].typedText)
}

func TestBrowserToolKeyRejectsUnknownKeyName(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "key", Key: "super-delete"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "key press failed")
}

func TestBrowserToolKeyAcceptsKnownKeyName(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "key", Key: "Enter"})
	require.False(t, resp.IsError)
	require.Equal(t, "enter", sessions.sessions["test-session"].pressedKey)
}

func TestBrowserToolEvalReturnsTheScriptResult(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "eval", Script: "1+41"})
	require.False(t, resp.IsError)
	require.Equal(t, "42", resp.Content)
}

func TestBrowserToolTextReturnsElementText(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "text", Selector: "body"})
	require.False(t, resp.IsError)
	require.Equal(t, "hello world", resp.Content)
}

func TestBrowserToolScreenshotReturnsAnImageResponse(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "screenshot"})
	require.False(t, resp.IsError)
	require.Equal(t, "image", resp.Type)
	require.Equal(t, "image/png", resp.MediaType)
	require.Equal(t, []byte("png-bytes"), resp.Data)
}

func TestBrowserToolCloseClosesTheSessionWithoutLaunchingOne(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sessions.sessions["test-session"] = &fakeBrowserSession{}

	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "close"})
	require.False(t, resp.IsError)
	require.Contains(t, sessions.closed, "test-session")
}

func TestBrowserToolBackAndForward(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "back"})
	require.False(t, resp.IsError)
	require.True(t, sessions.sessions["test-session"].wentBack)

	resp = runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "forward"})
	require.False(t, resp.IsError)
	require.True(t, sessions.sessions["test-session"].wentForward)
}

func TestBrowserToolClickPrefersRefOverSelector(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "click", Ref: "e3", Selector: "#ignored"})
	require.False(t, resp.IsError)
	require.Equal(t, `[data-atlas-ref="e3"]`, sessions.sessions["test-session"].clicked)
}

func TestBrowserToolClickRejectsAnUnsafeRef(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "click", Ref: `e3"]evil[`})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "invalid ref")
}

func TestBrowserToolTypeByRef(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "type", Ref: "e5", Text: "hi"})
	require.False(t, resp.IsError)
	require.Equal(t, `[data-atlas-ref="e5"]`, sessions.sessions["test-session"].typedSel)
	require.Equal(t, "hi", sessions.sessions["test-session"].typedText)
}

func TestBrowserToolScrollDefaultsAmount(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "scroll", Direction: "down"})
	require.False(t, resp.IsError)
	require.Equal(t, 0, sessions.sessions["test-session"].scrolledDX)
	require.Equal(t, defaultScrollAmount, sessions.sessions["test-session"].scrolledDY)
}

func TestBrowserToolScrollRejectsUnknownDirection(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "scroll", Direction: "sideways"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "unsupported direction")
}

func TestBrowserToolSnapshotFormatsElementsAndPendingDialogs(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sessions.sessions["test-session"] = &fakeBrowserSession{
		snapshot: []browser.SnapshotElement{
			{Ref: "e1", Role: "button", Tag: "button", Name: "Sign in"},
			{Ref: "e2", Role: "textbox", Tag: "input", Value: "foo@bar.com"},
		},
		dialogs: []browser.DialogInfo{{Type: "alert", Message: "heads up"}},
	}

	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "snapshot", Full: true})
	require.False(t, resp.IsError)
	require.True(t, sessions.sessions["test-session"].snapshotFull)
	require.Contains(t, resp.Content, `[e1] button "Sign in"`)
	require.Contains(t, resp.Content, `[e2] textbox`)
	require.Contains(t, resp.Content, `value="foo@bar.com"`)
	require.Contains(t, resp.Content, "pending dialog")
	require.Contains(t, resp.Content, "heads up")
}

func TestBrowserToolSnapshotEmpty(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "snapshot"})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "No interactive elements")
}

func TestBrowserToolImages(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sessions.sessions["test-session"] = &fakeBrowserSession{
		images: []browser.ImageInfo{{Src: "https://example.com/a.png", Alt: "a logo"}},
	}
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "images"})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "https://example.com/a.png")
	require.Contains(t, resp.Content, "a logo")
}

func TestBrowserToolConsole(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sessions.sessions["test-session"] = &fakeBrowserSession{
		console: []browser.ConsoleEntry{{Type: "error", Text: "boom"}},
	}
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "console"})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "error")
	require.Contains(t, resp.Content, "boom")
}

func TestBrowserToolConsoleEmpty(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "console"})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "No console output")
}

func TestBrowserToolDialogAcceptsWithPromptText(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "dialog", Accept: true, PromptText: "yes please"})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "Accepted")
	f := sessions.sessions["test-session"]
	require.NotNil(t, f.dialogAccept)
	require.True(t, *f.dialogAccept)
	require.Equal(t, "yes please", f.dialogText)
}

func TestBrowserToolDialogDismiss(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "dialog", Accept: false})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "Dismissed")
}

func TestBrowserToolDialogPropagatesError(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sessions.sessions["test-session"] = &fakeBrowserSession{dialogErr: fmt.Errorf("no pending dialog")}
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "dialog", Accept: true})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "no pending dialog")
}

func TestBrowserToolCDPRequiresMethod(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "cdp"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "cdp_method is required")
}

func TestBrowserToolCDPRejectsMalformedParams(t *testing.T) {
	t.Parallel()
	// A syntactically valid JSON value (a string) that is nonetheless
	// not an object -- an actually-malformed byte sequence can't survive
	// the test harness's own json.Marshal of the outer BrowserParams.
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{},
		BrowserParams{Action: "cdp", CDPMethod: "Network.getCookies", CDPParams: json.RawMessage(`"not an object"`)})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "cdp_params must be a JSON object")
}

func TestBrowserToolCDPSendsMethodAndParamsAndReturnsResult(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sessions.sessions["test-session"] = &fakeBrowserSession{
		cdpResult: map[string]any{"cookies": []any{}},
	}
	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{
		Action: "cdp", CDPMethod: "Network.getCookies", CDPParams: json.RawMessage(`{"urls":["https://example.com"]}`),
	})
	require.False(t, resp.IsError)
	f := sessions.sessions["test-session"]
	require.Equal(t, "Network.getCookies", f.cdpMethod)
	require.Equal(t, []any{"https://example.com"}, f.cdpParams["urls"])
	require.Contains(t, resp.Content, "cookies")
}

func TestBrowserToolRejectsUnknownAction(t *testing.T) {
	t.Parallel()
	resp := runBrowserTool(t, newFakeBrowserSessions(), &mockPermissionService{}, BrowserParams{Action: "teleport"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "unknown action")
}

func TestBrowserToolReportsLaunchFailureAsAToolError(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sessions.launchErr = fmt.Errorf("no chrome binary found")

	resp := runBrowserTool(t, sessions, &mockPermissionService{}, BrowserParams{Action: "navigate", URL: "https://example.com"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "failed to start browser")
}

// denyingPermissionService always denies, to verify the tool never touches
// the browser manager when permission is refused.
type denyingPermissionService struct {
	mockPermissionService
}

func (d *denyingPermissionService) Request(ctx context.Context, req permission.CreatePermissionRequest) (bool, error) {
	return false, nil
}

func TestBrowserToolNeverLaunchesASessionWhenPermissionIsDenied(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()

	resp := runBrowserTool(t, sessions, &denyingPermissionService{}, BrowserParams{Action: "navigate", URL: "https://example.com"})
	require.True(t, resp.IsError)
	require.Empty(t, sessions.sessions, "a denied call must not create a browser session")
}
