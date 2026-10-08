package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// These tests replay the failure modes of session aaae8f0a against the
// local figmalike.html fixture in a headless browser with a temporary
// profile. The fixture models editor mechanics; passing here is not proof
// that the same calls work in Figma.

type figmaModel struct {
	Layers []struct {
		ID       string  `json:"id"`
		Name     string  `json:"name"`
		Kind     string  `json:"kind"`
		FontSize float64 `json:"fontSize"`
		Width    float64 `json:"width"`
	} `json:"layers"`
	Selected string   `json:"selected"`
	Commits  []string `json:"commits"`
	Texts    []struct {
		Text string `json:"text"`
	} `json:"texts"`
}

func (l *liveTool) figma() figmaModel {
	l.t.Helper()
	raw, err := l.session().Eval("JSON.stringify(window.model)")
	require.NoError(l.t, err)
	var text string
	require.NoError(l.t, json.Unmarshal([]byte(raw), &text))
	var m figmaModel
	require.NoError(l.t, json.Unmarshal([]byte(text), &m))
	return m
}

func (m figmaModel) layer(id string) (float64, float64) {
	for _, l := range m.Layers {
		if l.ID == id {
			return l.FontSize, l.Width
		}
	}
	return -1, -1
}

func countCommits(commits []string, prefix string) int {
	n := 0
	for _, c := range commits {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func openFigmaLike(t *testing.T) *liveTool {
	t.Helper()
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/figmalike.html"))
	return l
}

// TestLiveFigmaLikeValueNamedFieldReadsBackOnce: the session's font-size
// write returned outcome_unknown because the field's name carries its value
// and the node was replaced. The same property of the same selection is
// resolved again and read back; the write happens exactly once.
func TestLiveFigmaLikeValueNamedFieldReadsBackOnce(t *testing.T) {
	l := openFigmaLike(t)
	resp := l.ok(`{"action":"set_field","advanced":{"role":"combobox","name":"Font size: 12 px","value":"14","commit":"enter"}}`)
	require.Contains(t, resp.Content, `"status":"applied"`)
	require.Contains(t, resp.Content, `"acceptance":"renamed_readback"`)
	require.Contains(t, resp.Content, `"verification":"verified"`)
	m := l.figma()
	size, _ := m.layer("1:2")
	require.Equal(t, 14.0, size)
	require.Equal(t, 1, countCommits(m.Commits, "fontSize:1:2"), "applied exactly once, never replayed")
}

// TestLiveFigmaLikeSelectionChangeIsNotSuccess: when the commit lands while
// the selection moves to another object that happens to show the requested
// value, the write is not reported as verified.
func TestLiveFigmaLikeSelectionChangeIsNotSuccess(t *testing.T) {
	l := openFigmaLike(t)
	l.eval(`window.switchSelectionOnCommit = true`)
	resp := l.run(`{"action":"set_field","advanced":{"role":"combobox","name":"Font size: 12 px","value":"14","commit":"enter"}}`)
	require.True(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "outcome_unknown")
	require.NotContains(t, resp.Content, `"verification":"verified"`)
	m := l.figma()
	require.Equal(t, "1:3", m.Selected, "the fixture did move the selection")
	require.Equal(t, 1, countCommits(m.Commits, "fontSize:"), "no blind retry")
}

// TestLiveFigmaLikeCanvasTextNeedsEditMode: insert_text with no editor
// focused sends nothing; after the text tool and a canvas click it types a
// long Turkish string exactly.
func TestLiveFigmaLikeCanvasTextNeedsEditMode(t *testing.T) {
	l := openFigmaLike(t)
	refused := l.run(`{"action":"insert_text","text":"q w e r t y"}`)
	require.True(t, refused.IsError)
	require.Contains(t, refused.Content, "focus_not_editable")
	require.Empty(t, l.figma().Texts)

	l.ok(`{"action":"key","key":"t"}`)
	shot := l.ok(`{"action":"screenshot"}`)
	var grant struct {
		ViewportID string  `json:"viewport_id"`
		Scale      float64 `json:"image_scale"`
	}
	_, after, ok := strings.Cut(shot.Content, "Viewport: ")
	require.True(t, ok, shot.Content)
	require.NoError(t, json.NewDecoder(strings.NewReader(after)).Decode(&grant))
	raw, err := l.session().Eval(`(()=>{const r=document.getElementById('canvas').getBoundingClientRect();return JSON.stringify({x:r.left+60,y:r.top+80})})()`)
	require.NoError(t, err)
	var text string
	require.NoError(t, json.Unmarshal([]byte(raw), &text))
	var at struct{ X, Y float64 }
	require.NoError(t, json.Unmarshal([]byte(text), &at))
	l.ok(fmt.Sprintf(`{"action":"pointer_click","advanced":{"viewport_id":%q,"x":%.0f,"y":%.0f}}`, grant.ViewportID, at.X*grant.Scale, at.Y*grant.Scale))
	long := "Bağlantı koptuğunda taslağın bu cihazda korunur; bilgisayardaki Atlas yeniden çevrimiçi olduğunda gönderebilirsin. Çok uzun Türkçe açıklama: ığüşöç İĞÜŞÖÇ."
	typed := l.ok(fmt.Sprintf(`{"action":"insert_text","text":%q}`, long))
	require.Contains(t, typed.Content, `"kind":"contenteditable"`)
	l.ok(`{"action":"key","key":"escape"}`)
	m := l.figma()
	require.Len(t, m.Texts, 1)
	require.Equal(t, long, m.Texts[0].Text)
}

// TestLiveFigmaLikeStaleRefRetargets: a re-rendered layer row invalidates
// its ref; the click is refused without input, a text= selector is
// refused with the semantic route, and semantic targeting by visible name
// selects the right layer.
func TestLiveFigmaLikeStaleRefRetargets(t *testing.T) {
	l := openFigmaLike(t)
	found := l.ok(`{"action":"find","advanced":{"role":"row","name":"06 · Oturumlar"}}`)
	var result struct {
		Matches []struct {
			Ref string `json:"ref"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(found.Content), &result), found.Content)
	require.Len(t, result.Matches, 1)
	l.eval(`rerenderRows()`)
	stale := l.run(fmt.Sprintf(`{"action":"click","ref":%q}`, result.Matches[0].Ref))
	require.True(t, stale.IsError)
	require.Contains(t, stale.Content, `"dispatch":"none"`)
	require.Equal(t, "1:2", l.figma().Selected)

	text := l.run(`{"action":"click","selector":"text=06 · Oturumlar"}`)
	require.True(t, text.IsError)
	require.Contains(t, text.Content, "unsupported_selector")

	l.ok(`{"action":"semantic_click","advanced":{"role":"row","name":"06 · Oturumlar"}}`)
	require.Equal(t, "1:3", l.figma().Selected)
}

// TestLiveFigmaLikePipelineResumesFromSafePoint: a pipeline that applied a
// write and then hit a vanished ref stops there; the follow-up sends only
// the remaining work and the first write is not applied a second time.
func TestLiveFigmaLikePipelineResumesFromSafePoint(t *testing.T) {
	l := openFigmaLike(t)
	pipeline := NewToolPipeline(func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return l.tool.Run(l.ctx, call)
	})
	found := l.ok(`{"action":"find","advanced":{"role":"row","name":"06 · Oturumlar"}}`)
	var result struct {
		Matches []struct {
			Ref string `json:"ref"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(found.Content), &result))
	l.eval(`rerenderRows()`)
	first := fmt.Sprintf(`{"steps":[{"id":"w","tool":"browser","arguments":{"action":"set_field","advanced":{"name":"Width","value":"390","commit":"enter"}}},{"id":"sel","tool":"browser","arguments":{"action":"click","ref":%q}},{"id":"size","tool":"browser","arguments":{"action":"set_field","advanced":{"role":"combobox","name":"Font size: 14 px","value":"16","commit":"enter"}}}]}`, result.Matches[0].Ref)
	resp, err := pipeline.Run(l.ctx, fantasy.ToolCall{ID: "p1", Name: "tool_pipeline", Input: first})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	var steps []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &steps), resp.Content)
	require.Equal(t, StepSucceeded, steps[0].Status)
	require.Equal(t, StepFailed, steps[1].Status)
	require.Equal(t, StepSkipped, steps[2].Status)
	require.Contains(t, steps[1].Resume, "Already applied, do not repeat: w")
	_, width := l.figma().layer("1:2")
	require.Equal(t, 390.0, width)

	second := `{"steps":[{"id":"sel","tool":"browser","arguments":{"action":"semantic_click","advanced":{"role":"row","name":"06 · Oturumlar"}}},{"id":"size","tool":"browser","arguments":{"action":"set_field","advanced":{"role":"combobox","name":"Font size: 14 px","value":"16","commit":"enter"}}}]}`
	resp, err = pipeline.Run(l.ctx, fantasy.ToolCall{ID: "p2", Name: "tool_pipeline", Input: second})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	m := l.figma()
	size, _ := m.layer("1:3")
	require.Equal(t, 16.0, size)
	require.Equal(t, 1, countCommits(m.Commits, "width:"), "the applied width write was not repeated")
}

// TestLiveFigmaLikeInlineRenameIsVerified: a page-name editor disappears on
// commit and leaves a row with the new name. That is the expected outcome of
// a rename, so it is verified by the new name appearing, not reported as
// outcome_unknown (which made the session log renames as uncertain).
func TestLiveFigmaLikeInlineRenameIsVerified(t *testing.T) {
	l := openFigmaLike(t)
	l.ok(`{"action":"semantic_click","advanced":{"role":"button","name":"Add new page"}}`)
	resp := l.ok(`{"action":"set_field","advanced":{"role":"textbox","name":"Page name","value":"ATLAS TEST · kabul","commit":"enter"}}`)
	require.Contains(t, resp.Content, `"acceptance":"inline_rename"`)
	require.Contains(t, resp.Content, `"verification":"verified"`)

	// A second rename to a name that already exists cannot be told apart
	// from the old row and stays honest.
	l.ok(`{"action":"semantic_click","advanced":{"role":"button","name":"Add new page"}}`)
	l.eval(`(()=>{const i=document.querySelector('[aria-label="Page name"]');i.addEventListener('keydown',e=>{if(e.key==='Enter'){e.stopImmediatePropagation();setTimeout(()=>i.remove(),80)}},{capture:true})})()`)
	dup := l.run(`{"action":"set_field","advanced":{"role":"textbox","name":"Page name","value":"ATLAS TEST · kabul","commit":"enter"}}`)
	require.True(t, dup.IsError, dup.Content)
	require.Contains(t, dup.Content, "outcome_unknown")
}

// TestLiveFigmaLikeContainsMatchesInnermost: matching visible text with
// match=contains used to return every ancestor (html, body, wrappers),
// 40 matches in the session; only the element holding the text remains.
func TestLiveFigmaLikeContainsMatchesInnermost(t *testing.T) {
	l := openFigmaLike(t)
	found := l.ok(`{"action":"find","advanced":{"name":"Oturumlar","match":"contains"}}`)
	var result struct {
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal([]byte(found.Content), &result), found.Content)
	require.Equal(t, 1, result.Count, found.Content)
	l.ok(`{"action":"semantic_click","advanced":{"name":"06 · Oturumlar"}}`)
	require.Equal(t, "1:3", l.figma().Selected)
}

// TestLiveFigmaLikeContentsRegionScopes: Figma's sidebars are
// display:contents regions with no box of their own; within and scope must
// still find them instead of reporting within_not_found/scope_not_unique.
func TestLiveFigmaLikeContentsRegionScopes(t *testing.T) {
	l := openFigmaLike(t)
	for _, target := range []string{`"within":"Properties"`, `"scope":"[aria-label=\"Properties\"]"`} {
		found := l.ok(`{"action":"find","advanced":{"name":"Width",` + target + `}}`)
		var result struct {
			Count int `json:"count"`
		}
		require.NoError(t, json.Unmarshal([]byte(found.Content), &result), found.Content)
		require.Equal(t, 1, result.Count, found.Content)
	}
	l.ok(`{"action":"set_field","advanced":{"name":"Width","within":"Properties","value":"375","commit":"enter"}}`)
	_, width := l.figma().layer("1:2")
	require.Equal(t, 375.0, width)
}

// TestLiveFigmaLikeButtonTextWrapperKeepsButton: Figma renders button
// labels inside a zero-size <i18n-text> element. Matching by visible text
// must resolve the button, not its wrapper, so the click lands.
func TestLiveFigmaLikeButtonTextWrapperKeepsButton(t *testing.T) {
	l := openFigmaLike(t)
	found := l.ok(`{"action":"find","advanced":{"name":"Create style"}}`)
	var result struct {
		Count   int `json:"count"`
		Matches []struct {
			Tag string `json:"tag"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(found.Content), &result), found.Content)
	require.Equal(t, 1, result.Count, found.Content)
	require.Equal(t, "BUTTON", result.Matches[0].Tag)
	l.ok(`{"action":"semantic_click","advanced":{"name":"Create style","within":"Variable and style creation"}}`)
	raw, err := l.session().Eval("model.styles")
	require.NoError(t, err)
	require.Equal(t, "1", raw)
}

// TestLiveFigmaLikeButtonComboboxReadsDisplayedValue: Figma's variant
// picker is a <button role=combobox>. Its value property is always empty,
// so select_option reported not_applied after choosing the option.
func TestLiveFigmaLikeButtonComboboxReadsDisplayedValue(t *testing.T) {
	l := openFigmaLike(t)
	resp := l.ok(`{"action":"select_option","advanced":{"role":"combobox","name":"Role","within":"Instance","value":"User"}}`)
	require.Contains(t, resp.Content, `"accepted_value":"User"`)
	raw, err := l.session().Eval("model.role")
	require.NoError(t, err)
	require.Equal(t, `"User"`, raw)
}

// TestLiveFigmaLikeOptionOfOpenedList: Figma's horizontal and vertical
// resizing menus both offer "Fill container"; select_option refused as
// ambiguous. The option of the list the control owns or sits next to is
// chosen, and only that menu changes.
func TestLiveFigmaLikeOptionOfOpenedList(t *testing.T) {
	l := openFigmaLike(t)
	resp := l.ok(`{"action":"select_option","advanced":{"role":"combobox","name":"Horizontal resizing","within":"Resizing","value":"Fill container"}}`)
	require.Contains(t, resp.Content, `"accepted_value":"Fill container"`)
	raw, err := l.session().Eval(`JSON.stringify([model.hr || "", model.vr || ""])`)
	require.NoError(t, err)
	require.Equal(t, `"[\"Fill container\",\"\"]"`, raw)
}

// TestLiveFigmaLikeParkedOptionsAndOpenList: Figma's W field opens its
// menu from a chevron, not the input, and keeps closed menus rendered above
// the page. With the list opened by its trigger, select_option picks the
// one reachable option instead of a parked one and does not toggle the
// list shut. The field keeps its number; the mode shown beside it is the
// readback.
func TestLiveFigmaLikeParkedOptionsAndOpenList(t *testing.T) {
	l := openFigmaLike(t)
	// In Figma only the opened list is reachable; the always-open resizing
	// lists of the other tests are not part of this case.
	_, err := l.session().Eval(`document.querySelector('fieldset').remove(); document.getElementById('w-chevron').click()`)
	require.NoError(t, err)
	resp := l.ok(`{"action":"select_option","advanced":{"role":"combobox","name":"Width mode","value":"Fill container"}}`)
	require.Contains(t, resp.Content, `"accepted_value":"254"`)
	require.Contains(t, resp.Content, `"acceptance":"shown_mode"`)
	raw, err := l.session().Eval(`model.wm || ""`)
	require.NoError(t, err)
	require.Equal(t, `"Fill container"`, raw)
}

// TestLiveFigmaLikeRemountedFieldReadsBack: an application rebuilds a
// field after its commit: the edited node is reused for another property
// and the new field appears later, under the same name. The rebuilt
// field of the same selection is read back instead of reporting
// outcome_unknown, and the value is committed once.
func TestLiveFigmaLikeRemountedFieldReadsBack(t *testing.T) {
	l := openFigmaLike(t)
	resp := l.ok(`{"action":"set_field","advanced":{"name":"Height","within":"Right sidebar","value":"844","commit":"enter"}}`)
	require.Contains(t, resp.Content, `"accepted_value":"844"`)
	require.Contains(t, resp.Content, `"acceptance":"remount_readback"`)
	m := l.figma()
	require.Equal(t, 1, countCommits(m.Commits, "height:"))
}

// TestLiveFigmaLikeRelabeledFieldIsNamed: Figma renames the size field
// "Vertical resizing" to "Height" when a number fixes the size. Whether
// that is the same property is not claimed; the error names the new label
// and the shown value, and that label reads back the value.
func TestLiveFigmaLikeRelabeledFieldIsNamed(t *testing.T) {
	l := openFigmaLike(t)
	resp := l.run(`{"action":"set_field","advanced":{"role":"combobox","name":"Vertical resizing","within":"Right sidebar","value":"844","commit":"enter"}}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, `outcome_unknown: the edited field is now named "Height" and shows "844"`)
	require.Contains(t, resp.Content, `"relabeled_as":"Height"`)
	read := l.ok(`{"action":"read_field","advanced":{"role":"combobox","name":"Height","within":"Right sidebar"}}`)
	require.Contains(t, read.Content, `"accepted_value":"844"`)
	require.Equal(t, 1, countCommits(l.figma().Commits, "fixedH:"))
}

// TestLiveFigmaLikeTwinLabelIsOneTarget: Figma draws its "Prototype" tab
// label twice in the same box, and semantic_click refused it as ambiguous.
// The same box, role and name is one place to click.
func TestLiveFigmaLikeTwinLabelIsOneTarget(t *testing.T) {
	l := openFigmaLike(t)
	l.ok(`{"action":"semantic_click","advanced":{"name":"Prototype"}}`)
	raw, err := l.session().Eval(`model.tab || ""`)
	require.NoError(t, err)
	require.Equal(t, `"prototype"`, raw)
}
