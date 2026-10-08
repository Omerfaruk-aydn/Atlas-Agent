package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/stretchr/testify/require"
)

// These tests replay the text sequence of session 14b5c83a (calls 40-62)
// against canvastext.html, whose text proxy keeps focus after escape the way
// the session observed in Figma. The fixture models mechanics only; passing
// here is not proof about Figma itself.

type canvasTextModel struct {
	Tool  string `json:"tool"`
	Texts []struct {
		ID   string  `json:"id"`
		Text string  `json:"text"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
	} `json:"texts"`
}

func TestLiveScreenshotAuthorizesItsDisplayedReferences(t *testing.T) {
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/canvastext.html"))
	// A reconnect has no observation grants from the old process. A
	// screenshot's own labels must be sufficient without another observe.
	browserObservations.invalidate(l.id)
	l.ok(`{"action":"screenshot","annotate":false}`)
	refs, err := l.session().Snapshot(false)
	require.NoError(t, err)
	textRef := ""
	for _, el := range refs {
		if el.Role == "button" && el.Name == "Text" {
			textRef = el.Ref
		}
	}
	require.NotEmpty(t, textRef)
	denied := l.run(fmt.Sprintf(`{"action":"click","ref":%q}`, textRef))
	require.True(t, denied.IsError, "A clean image must not grant unseen refs")
	require.Contains(t, denied.Content, "stale_ref")
	l.ok(`{"action":"screenshot"}`)
	capture, ok := l.session().(browser.ScreenshotRefSession)
	require.True(t, ok)
	_, shown := capture.ScreenshotReferences()
	require.Equal(t, browser.RefIdentity{Role: "button", Name: "Text"}, shown[textRef])
	l.ok(fmt.Sprintf(`{"action":"click","ref":%q}`, textRef))
	// The ref still cannot act on a node reused for another control.
	l.eval(`document.querySelector('#tool-text').setAttribute('aria-label','Other')`)
	reused := l.run(fmt.Sprintf(`{"action":"click","ref":%q}`, textRef))
	require.True(t, reused.IsError)
	require.Contains(t, reused.Content, "stale_ref")
}

func (l *liveTool) canvasText() canvasTextModel {
	l.t.Helper()
	raw, err := l.session().Eval("JSON.stringify(window.model)")
	require.NoError(l.t, err)
	var text string
	require.NoError(l.t, json.Unmarshal([]byte(raw), &text))
	var m canvasTextModel
	require.NoError(l.t, json.Unmarshal([]byte(text), &m))
	return m
}

type viewportGrant struct {
	ViewportID string  `json:"viewport_id"`
	Scale      float64 `json:"image_scale"`
}

func (l *liveTool) viewport() viewportGrant {
	l.t.Helper()
	shot := l.ok(`{"action":"screenshot"}`)
	_, after, ok := strings.Cut(shot.Content, "Viewport: ")
	require.True(l.t, ok, shot.Content)
	var grant viewportGrant
	require.NoError(l.t, json.NewDecoder(strings.NewReader(after)).Decode(&grant))
	return grant
}

// canvasClick clicks canvas-local CSS coordinates through the screenshot
// grant, as a model aiming from the image would.
func (l *liveTool) canvasClick(grant viewportGrant, x, y float64, count int) string {
	l.t.Helper()
	page := func(v, offset float64) float64 { return (v + offset) * grant.Scale }
	input := fmt.Sprintf(`{"action":"pointer_click","advanced":{"viewport_id":%q,"x":%.0f,"y":%.0f,"click_count":%d}}`, grant.ViewportID, page(x, 0), page(y, 36), count)
	return l.ok(input).Content
}

func testCanvasTextSequence(t *testing.T, page string) {
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+page))
	l.ok(`{"action":"semantic_click","advanced":{"role":"button","name":"Text"}}`)
	grant := l.viewport()
	l.canvasClick(grant, 200, 100, 1)
	first := l.ok(`{"action":"insert_text","text":"ATLAS"}`)
	require.Contains(t, first.Content, `"editor_text_after":"ATLAS"`, first.Content)
	l.ok(`{"action":"key","key":"escape"}`)

	// Call 44: "t" meant as the text-tool shortcut while the proxy kept
	// focus. It is refused instead of being typed into the ATLAS layer.
	key := l.run(`{"action":"key","key":"t"}`)
	require.True(t, key.IsError)
	require.Contains(t, key.Content, "key_would_type")
	// Call 45: insert_text into the proxy after escape. The proxy is still
	// focused and editable, but no text-edit session is open.
	stale := l.run(`{"action":"insert_text","text":"Remote Control"}`)
	require.True(t, stale.IsError)
	require.Contains(t, stale.Content, "text_edit_not_open")
	m := l.canvasText()
	require.Len(t, m.Texts, 1)
	require.Equal(t, "ATLAS", m.Texts[0].Text, "nothing was appended to the existing layer")

	// The supported route: tool button, empty canvas point, text.
	l.ok(`{"action":"semantic_click","advanced":{"role":"button","name":"Text"}}`)
	l.canvasClick(grant, 200, 220, 1)
	l.ok(`{"action":"insert_text","text":"Remote Control"}`)
	l.ok(`{"action":"key","key":"escape"}`)
	m = l.canvasText()
	require.Len(t, m.Texts, 2)
	require.Equal(t, "ATLAS", m.Texts[0].Text)
	require.Equal(t, "Remote Control", m.Texts[1].Text)

	// Editing existing text is distinct from creating text: without mode
	// append the insertion is refused, with it the text is extended.
	l.canvasClick(grant, 205, 105, 2)
	refused := l.run(`{"action":"insert_text","text":" Remote"}`)
	require.True(t, refused.IsError)
	require.Contains(t, refused.Content, "text_editor_not_empty")
	appended := l.ok(`{"action":"insert_text","text":" Remote","advanced":{"mode":"append"}}`)
	require.Contains(t, appended.Content, `"editor_text_before":"ATLAS"`)
	require.Equal(t, "ATLAS Remote", l.canvasText().Texts[0].Text)
}

func TestLiveCanvasTextHiddenProxy(t *testing.T) {
	testCanvasTextSequence(t, "/canvastext.html")
}

func TestLiveCanvasTextOverlayEditor(t *testing.T) {
	testCanvasTextSequence(t, "/canvastext.html?overlay")
}

func TestLiveCanvasTextDirectionalEmptyProxy(t *testing.T) {
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/canvastext.html"))
	// Figma returns these directional isolation marks for a new empty
	// editor. Reproduce that DOM state through the real browser tool.
	_, err := l.session().Eval(`document.querySelector('#text-proxy').addEventListener('focus', function(){if(!this.textContent)this.textContent='\u2068\u2069'})`)
	require.NoError(t, err)
	l.ok(`{"action":"semantic_click","advanced":{"role":"button","name":"Text"}}`)
	l.canvasClick(l.viewport(), 200, 100, 1)
	result := l.ok(`{"action":"insert_text","text":"ATLAS"}`)
	require.Contains(t, result.Content, "ATLAS")
	require.Len(t, l.canvasText().Texts, 1)
	require.Contains(t, l.canvasText().Texts[0].Text, "ATLAS")
}
