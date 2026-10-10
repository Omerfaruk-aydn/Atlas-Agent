package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// liveBrowserExecutable finds an installed Chromium-family browser. A skip
// is not browser proof; ATLAS_REQUIRE_BROWSER turns it into a failure.
func liveBrowserExecutable(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("Real browser excluded in short mode")
	}
	for _, path := range []string{"C:/Program Files/Google/Chrome/Application/chrome.exe", "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "/usr/bin/chromium", "/usr/bin/google-chrome"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	if os.Getenv("ATLAS_REQUIRE_BROWSER") != "" {
		t.Fatal("ATLAS_REQUIRE_BROWSER is set but no browser was found")
	}
	t.Skip("Real browser unavailable; this is not passing browser proof")
	return ""
}

// liveTool runs the real browser tool against a headless browser serving
// the local fixtures. Nothing touches a user browser, account or profile.
type liveTool struct {
	t       *testing.T
	tool    fantasy.AgentTool
	manager *browser.Manager
	ctx     context.Context
	id      string
	base    string
	root    string
	calls   int
}

func newLiveTool(t *testing.T) *liveTool {
	t.Helper()
	exe := liveBrowserExecutable(t)
	server := httptest.NewServer(http.FileServer(http.Dir("../../browser/testdata/browseruse")))
	t.Cleanup(server.Close)
	profile := t.TempDir()
	manager := browser.GetManager(browser.Options{ExecutablePath: exe, Headless: true, UserDataDir: profile, OverlayDisabled: true, ActionTimeout: 60 * time.Second})
	root := t.TempDir()
	id := "live-" + t.Name()
	t.Cleanup(func() {
		manager.Close(id)
		// Chrome's crash reporter can briefly retain files after browser exit.
		// Retry only this test-owned profile before TempDir's final cleanup.
		var cleanupErr error
		require.Eventually(t, func() bool {
			cleanupErr = os.RemoveAll(profile)
			return cleanupErr == nil
		}, 10*time.Second, 50*time.Millisecond, "browser profile cleanup: %v", cleanupErr)
	})
	return &liveTool{t: t, tool: newBrowserTool(&mockPermissionService{}, root, manager, browserDescription(false)), manager: manager, ctx: context.WithValue(t.Context(), SessionIDContextKey, id), id: id, base: server.URL, root: root}
}

func (l *liveTool) run(input string) fantasy.ToolResponse {
	l.t.Helper()
	l.calls++
	resp, err := l.tool.Run(l.ctx, fantasy.ToolCall{ID: fmt.Sprintf("live-%d", l.calls), Name: BrowserToolName, Input: input})
	require.NoError(l.t, err)
	return resp
}

func (l *liveTool) ok(input string) fantasy.ToolResponse {
	l.t.Helper()
	resp := l.run(input)
	require.False(l.t, resp.IsError, "%s -> %s", input, resp.Content)
	return resp
}

func (l *liveTool) session() browser.Session {
	l.t.Helper()
	sess, err := l.manager.Session(l.id)
	require.NoError(l.t, err)
	return sess
}

// state reads the panel fixture's model in the selected tab.
func (l *liveTool) state() map[string]map[string]float64 {
	l.t.Helper()
	raw, err := l.session().Eval("JSON.stringify(window.cards)")
	require.NoError(l.t, err)
	var text string
	require.NoError(l.t, json.Unmarshal([]byte(raw), &text))
	var out map[string]map[string]float64
	require.NoError(l.t, json.Unmarshal([]byte(text), &out))
	return out
}

func (l *liveTool) eval(script string) {
	l.t.Helper()
	_, err := l.session().Eval(script)
	require.NoError(l.t, err)
}

const setWidth = `{"action":"set_field","advanced":{"label":"Width","within":"Design","value":%q,"commit":"blur"}}`

// TestLiveUndoRestoresOnlyTheEditedObject is the regression for undo
// changing whatever object the reused property panel shows now.
func TestLiveUndoRestoresOnlyTheEditedObject(t *testing.T) {
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panel.html?doc=sel&bw=300"))
	l.ok(fmt.Sprintf(setWidth, "300"))
	require.Equal(t, 300.0, l.state()["a"]["w"])

	// Card B is selected; the same Width field now shows B's width, which
	// equals the value Atlas set on A.
	l.ok(`{"action":"semantic_click","advanced":{"role":"option","name":"Card B"}}`)
	refused := l.run(`{"action":"undo"}`)
	require.True(t, refused.IsError)
	require.Contains(t, refused.Content, "undo_unsafe")
	require.Contains(t, refused.Content, "selected object changed")
	require.Equal(t, map[string]float64{"w": 300, "h": 50}, l.state()["a"])
	require.Equal(t, map[string]float64{"w": 300, "h": 80}, l.state()["b"], "the other card is untouched")

	// The record survived the refusal: back on Card A the undo succeeds and
	// is verified by readback.
	l.ok(`{"action":"semantic_click","advanced":{"role":"option","name":"Card A"}}`)
	undone := l.ok(`{"action":"undo"}`)
	require.Contains(t, undone.Content, `"undone":true`)
	require.Contains(t, undone.Content, `"verification":"verified"`)
	require.Equal(t, 100.0, l.state()["a"]["w"])
	require.Equal(t, 300.0, l.state()["b"]["w"])
	again := l.run(`{"action":"undo"}`)
	require.Contains(t, again.Content, "undo_unavailable", "an undone edit is consumed")
}

// TestLiveUndoRefusesOtherTabsAndDocuments covers same-origin tabs,
// another document and a reload of the same URL with the same value.
func TestLiveUndoRefusesOtherTabsAndDocuments(t *testing.T) {
	l := newLiveTool(t)
	first := l.base + "/panel.html?doc=t1"
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, first))
	l.ok(fmt.Sprintf(setWidth, "300"))
	work := l.session().(browser.TabIdentity).ActiveTab()

	// Another tab of the same origin whose field holds the same value.
	l.ok(`{"action":"tab_new","advanced":{"purpose":"compare"}}`)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panel.html?doc=t2&aw=300"))
	refused := l.run(`{"action":"undo"}`)
	require.Contains(t, refused.Content, "undo_unsafe")
	require.Contains(t, refused.Content, "another tab")
	require.Equal(t, 300.0, l.state()["a"]["w"])

	l.ok(`{"action":"tabs"}`)
	l.ok(fmt.Sprintf(`{"action":"tab_select","advanced":{"tab_id":%q}}`, work))
	// Another document of the same origin in the edit's own tab.
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panel.html?doc=t3&aw=300"))
	refused = l.run(`{"action":"undo"}`)
	require.Contains(t, refused.Content, "undo_unsafe")
	require.Contains(t, refused.Content, "another document")
	require.Equal(t, 300.0, l.state()["a"]["w"])

	// The same URL again: the stored value is the one Atlas set, but the
	// document is a new one.
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, first))
	require.Equal(t, 300.0, l.state()["a"]["w"])
	refused = l.run(`{"action":"undo"}`)
	require.Contains(t, refused.Content, "undo_unsafe")
	require.Contains(t, refused.Content, "another document")
	require.Equal(t, 300.0, l.state()["a"]["w"])
}

// TestLiveUndoRespectsUserChangesAndMissingTargets keeps intervening user
// edits and keeps the record when the field cannot be resolved.
func TestLiveUndoRespectsUserChangesAndMissingTargets(t *testing.T) {
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panel.html?doc=u"))
	l.ok(fmt.Sprintf(setWidth, "300"))
	// The user types another value afterwards.
	l.eval(`(()=>{const w=document.getElementById('w');w.value='333';w.dispatchEvent(new Event('change'))})()`)
	refused := l.run(`{"action":"undo"}`)
	require.Contains(t, refused.Content, "undo_unsafe")
	require.Contains(t, refused.Content, "someone else edited it")
	require.Equal(t, 333.0, l.state()["a"]["w"], "the user's value is kept")

	// A new edit, then the field disappears: nothing is changed and the
	// record is kept until the field is back.
	l.ok(fmt.Sprintf(setWidth, "310"))
	l.eval(`document.getElementById('w').style.display='none'`)
	missing := l.run(`{"action":"undo"}`)
	require.Contains(t, missing.Content, "undo_unavailable")
	require.Contains(t, missing.Content, `"record_kept":true`)
	require.Equal(t, 310.0, l.state()["a"]["w"])
	l.eval(`document.getElementById('w').style.display=''`)
	l.ok(`{"action":"undo"}`)
	require.Equal(t, 333.0, l.state()["a"]["w"], "the value before Atlas's edit is restored")

	// Another field of the panel changed after the edit: the panel may show
	// another object, so the undo is refused rather than guessed.
	l.ok(fmt.Sprintf(setWidth, "320"))
	l.eval(`(()=>{const h=document.getElementById('h');h.value='55';h.dispatchEvent(new Event('change'))})()`)
	refused = l.run(`{"action":"undo"}`)
	require.Contains(t, refused.Content, "undo_unsafe")
	require.Equal(t, 320.0, l.state()["a"]["w"])
}
