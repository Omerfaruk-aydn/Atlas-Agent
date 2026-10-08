package tools

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveUndoDistinguishesDuplicateObjectNames(t *testing.T) {
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panel.html?doc=duplicate&bw=300"))
	l.eval(`(()=>{document.getElementById('opt-a').textContent='Rectangle';document.getElementById('opt-b').textContent='Rectangle';cards.b.h=50;render()})()`)
	l.ok(fmt.Sprintf(setWidth, "300"))
	l.ok(`{"action":"semantic_click","advanced":{"selector":"#opt-b"}}`)
	refused := l.run(`{"action":"undo"}`)
	require.True(t, refused.IsError, refused.Content)
	require.Contains(t, refused.Content, "undo_unsafe")
	require.Equal(t, 300.0, l.state()["b"]["w"], "the identically named other object is untouched")
	l.ok(`{"action":"semantic_click","advanced":{"selector":"#opt-a"}}`)
	l.ok(`{"action":"undo"}`)
	require.Equal(t, 100.0, l.state()["a"]["w"], "returning to the actual edited node allows undo")
	require.Equal(t, 300.0, l.state()["b"]["w"])
}

func TestLiveUndoDetectsReusedSelectionNode(t *testing.T) {
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panel.html?doc=reused&bw=300"))
	l.eval(`(()=>{document.getElementById('opt-a').setAttribute('data-object-id','a');cards.b.h=50})()`)
	l.ok(fmt.Sprintf(setWidth, "300"))
	// A virtualized row keeps its DOM node and display name but now points
	// to another application object with exactly the same property values.
	l.eval(`(()=>{selected='b';w.value=cards.b.w;h.value=cards.b.h;document.getElementById('opt-a').setAttribute('data-object-id','b')})()`)
	r := l.run(`{"action":"undo"}`)
	require.True(t, r.IsError, r.Content)
	require.Contains(t, r.Content, "undo_unsafe")
	require.Equal(t, 300.0, l.state()["b"]["w"])
}

func TestLiveFillFormStopsAfterNavigation(t *testing.T) {
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panel.html?doc=form-original"))
	l.eval(fmt.Sprintf(`document.getElementById('w').addEventListener('change',()=>location.assign(%q),{once:true})`, l.base+"/panel.html?doc=form-other"))
	r := l.run(`{"action":"fill_form","advanced":{"fields":[{"label":"Width","within":"Design","value":"300","commit":"blur"},{"label":"Height","within":"Design","value":"999","commit":"blur"}]}}`)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "stale_document")
	require.Contains(t, r.Content, "not_attempted")
	require.Equal(t, 50.0, l.state()["a"]["h"], "no later field is written into the new document")
}

func TestLiveFillFormInvalidatesEvidenceIncludingPartialFailure(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%v", partial), func(t *testing.T) {
			l := newLiveTool(t)
			l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panel.html?doc=evidence"))
			l.ok(`{"action":"task_plan","task":{"task_id":"review","requirements":[{"id":"width","text":"Width is 300"}]}}`)
			l.ok(fmt.Sprintf(setWidth, "300"))
			// Read through a selector, then change through a label: evidence
			// follows the resolved field, not the spelling of the target.
			op := opOf(t, l.ok(`{"action":"read_field","advanced":{"selector":"#w"}}`).Content)
			l.ok(fmt.Sprintf(`{"action":"task_update","task":{"id":"width","status":"verified","evidence":[%q]}}`, op.ID))
			fields := `{"label":"Width","within":"Design","value":"400","commit":"blur"}`
			if partial {
				fields += `,{"label":"Missing","value":"unused"}`
			}
			result := l.run(`{"action":"fill_form","advanced":{"fields":[` + fields + `]}}`)
			require.Equal(t, partial, result.IsError, result.Content)
			require.Equal(t, 400.0, l.state()["a"]["w"])
			status := l.ok(`{"action":"task_status"}`)
			var view taskView
			require.NoError(t, json.Unmarshal([]byte(status.Content), &view))
			require.Equal(t, 1, view.Open, status.Content)
			require.Equal(t, reqReverify, view.Requirements[0].Effective)
			old := l.run(fmt.Sprintf(`{"action":"task_update","task":{"id":"width","status":"verified","evidence":[%q]}}`, op.ID))
			require.True(t, old.IsError, "old evidence cannot be attached again")
			_, cont := BrowserTaskContinuation(l.root, l.id)
			require.True(t, cont, "the old verification must not let the completion gate finish")
		})
	}
}

func TestLiveFoundRefsAreUsableWithoutFullObservation(t *testing.T) {
	l := newLiveTool(t)
	require.NoError(t, l.session().Navigate(l.base+"/panel.html?doc=find"))
	found := l.ok(`{"action":"find","advanced":{"label":"Width","within":"Design"}}`)
	var result struct {
		Matches []struct {
			Ref string `json:"ref"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(found.Content), &result))
	require.Len(t, result.Matches, 1)
	l.ok(fmt.Sprintf(`{"action":"set_field","ref":%q,"advanced":{"value":"300","commit":"blur"}}`, result.Matches[0].Ref))
	require.Equal(t, 300.0, l.state()["a"]["w"])
}

func TestLiveUnchangedFormPreservesEvidence(t *testing.T) {
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panel.html?doc=unchanged"))
	l.ok(`{"action":"task_plan","task":{"task_id":"same","requirements":[{"id":"width","text":"Width is 300"}]}}`)
	op := opOf(t, l.ok(fmt.Sprintf(setWidth, "300")).Content)
	l.ok(fmt.Sprintf(`{"action":"task_update","task":{"id":"width","status":"verified","evidence":[%q]}}`, op.ID))
	l.ok(`{"action":"fill_form","advanced":{"only_invalid":true,"fields":[{"label":"Width","within":"Design","value":"300"}]}}`)
	status := l.ok(`{"action":"task_status"}`)
	var view taskView
	require.NoError(t, json.Unmarshal([]byte(status.Content), &view))
	require.Zero(t, view.Open, status.Content)
	require.Empty(t, view.Requirements[0].Effective)
}
