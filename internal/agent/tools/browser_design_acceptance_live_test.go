package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveRenameRejectsUnrelatedText(t *testing.T) {
	l := openFigmaLike(t)
	l.ok(`{"action":"semantic_click","advanced":{"role":"button","name":"Add new page"}}`)
	l.eval(`(()=>{const i=document.querySelector('[aria-label="Page name"]');i.addEventListener('keydown',e=>{if(e.key==='Enter'){e.stopImmediatePropagation();setTimeout(()=>{i.remove();const b=document.createElement('button');b.textContent='Expected new page';document.getElementById('right').appendChild(b)},50)}},{capture:true})})()`)
	r := l.run(`{"action":"set_field","advanced":{"role":"textbox","name":"Page name","value":"Expected new page","commit":"enter"}}`)
	require.True(t, r.IsError, "a matching unrelated button does not prove the page was renamed: %s", r.Content)
}

func TestLiveRemountRejectsInvalidField(t *testing.T) {
	l := openFigmaLike(t)
	l.eval(`(()=>{const i=document.querySelector('[aria-label="Height"]');i.addEventListener('keydown',e=>{if(e.key==='Enter'){e.stopImmediatePropagation();i.setAttribute('aria-label','Minimum height');i.value='';setTimeout(()=>{const n=document.createElement('input');n.setAttribute('aria-label','Height');n.setAttribute('aria-invalid','true');n.setAttribute('aria-errormessage','review-error');n.value='844';i.replaceWith(n);const err=document.createElement('span');err.id='review-error';err.textContent='Height rejected';document.getElementById('right').appendChild(err)},450)}},{capture:true})})()`)
	r := l.run(`{"action":"set_field","advanced":{"name":"Height","within":"Right sidebar","value":"844","commit":"enter"}}`)
	require.True(t, r.IsError, "an invalid remounted value cannot be verified: %s", r.Content)
}

func TestLiveSelectionNoOpIsNotProgress(t *testing.T) {
	l := openFigmaLike(t)
	l.ok(`{"action":"task_plan","task":{"task_id":"review","requirements":[{"id":"design","text":"Build UI","verify":"structural"}]}}`)
	l.ok(`{"action":"semantic_click","advanced":{"role":"row","name":"01 · Sohbet"}}`)
	r := l.ok(`{"action":"task_status"}`)
	require.Contains(t, r.Content, `"progress":0`, "clicking the already selected row did not deliver anything: %s", r.Content)
}

func TestLiveRemountWaitsForLateRejection(t *testing.T) {
	l := openFigmaLike(t)
	l.eval(`(()=>{const i=document.querySelector('[aria-label="Height"]');i.addEventListener('keydown',e=>{if(e.key==='Enter'){e.stopImmediatePropagation();i.setAttribute('aria-label','Minimum height');i.value='';setTimeout(()=>{const n=document.createElement('input');n.setAttribute('aria-label','Height');n.value='844';i.replaceWith(n);setTimeout(()=>{n.value='483';n.setAttribute('aria-invalid','true')},200)},450)}},{capture:true})})()`)
	r := l.run(`{"action":"set_field","advanced":{"name":"Height","within":"Right sidebar","value":"844","commit":"enter","settle_ms":1000}}`)
	require.True(t, r.IsError, r.Content)
	require.Contains(t, r.Content, `"accepted_value":"483"`)
	require.NotContains(t, r.Content, `"verification":"verified"`)
}

func TestLiveNoOpFieldIsNotProgress(t *testing.T) {
	l := openFigmaLike(t)
	l.ok(`{"action":"task_plan","task":{"task_id":"review","requirements":[{"id":"design","text":"Build UI"}]}}`)
	l.ok(`{"action":"set_field","advanced":{"name":"Width","value":"360","commit":"enter"}}`)
	r := l.ok(`{"action":"task_status"}`)
	require.Contains(t, r.Content, `"progress":0`, r.Content)
	l.ok(`{"action":"set_field","advanced":{"name":"Width","value":"390","commit":"enter"}}`)
	r = l.ok(`{"action":"task_status"}`)
	require.Contains(t, r.Content, `"progress":1`, r.Content)
}

// TestLiveObservationClipsSidebarRows keeps annotations inside the painted
// sidebar instead of covering unrelated canvas content.
func TestLiveObservationClipsSidebarRows(t *testing.T) {
	l := openFigmaLike(t)
	l.eval(`document.body.innerHTML='<aside style="position:absolute;left:10px;top:10px;width:180px;height:100px;overflow:hidden"><div role="row" aria-label="Long layer" style="width:1200px;height:30px">Long layer</div><div role="row" aria-label="Hidden layer" style="position:absolute;top:150px;width:1200px;height:30px">Hidden layer</div></aside>'`)
	elements, err := l.session().Snapshot(false)
	require.NoError(t, err)
	found := false
	for _, e := range elements {
		require.NotEqual(t, "Hidden layer", e.Name)
		if e.Name == "Long layer" {
			found = true
			require.LessOrEqual(t, e.Rect.X+e.Rect.Width, 190.0)
			require.LessOrEqual(t, e.Rect.Width, 180.0)
		}
	}
	require.True(t, found)
}

func TestLiveSelectOptionOpensSeparateTrigger(t *testing.T) {
	l := openFigmaLike(t)
	l.eval(`document.querySelector('fieldset').remove()`)
	r := l.ok(`{"action":"select_option","advanced":{"role":"combobox","name":"Width mode","value":"Fill container"}}`)
	require.Contains(t, r.Content, `"acceptance":"shown_mode"`)
	require.Contains(t, r.Content, `"accepted_value":"254"`)
}

func TestLiveSelectOptionRejectsAmbiguousTriggers(t *testing.T) {
	l := openFigmaLike(t)
	l.eval(`(()=>{document.querySelector('fieldset').remove();const b=document.getElementById('w-chevron');const extra=b.cloneNode(true);extra.id='other-chevron';b.after(extra)})()`)
	r := l.run(`{"action":"select_option","advanced":{"role":"combobox","name":"Width mode","value":"Fill container"}}`)
	require.True(t, r.IsError, r.Content)
	require.Contains(t, r.Content, "ambiguous_target")
	require.Contains(t, r.Content, `"dispatch":"none"`)
}

func TestLiveSelectOptionDoesNotVerifyRejectedMode(t *testing.T) {
	l := openFigmaLike(t)
	l.eval(`(()=>{document.querySelector('fieldset').remove();document.getElementById('w-popup').addEventListener('click',()=>setTimeout(()=>document.getElementById('wm').setAttribute('aria-invalid','true'),400))})()`)
	r := l.run(`{"action":"select_option","advanced":{"role":"combobox","name":"Width mode","value":"Fill container","settle_ms":1000}}`)
	require.True(t, r.IsError, r.Content)
	require.Contains(t, r.Content, "value_not_applied")
	require.NotContains(t, r.Content, `"verification":"verified"`)
}

func TestLiveSetFieldRejectsInvalidRetainedValue(t *testing.T) {
	l := openFigmaLike(t)
	l.eval(`document.body.innerHTML='<label>Width<input aria-label="Width" value="360"></label>';document.querySelector('input').addEventListener('keydown',e=>{if(e.key==='Enter')e.target.setAttribute('aria-invalid','true')})`)
	r := l.run(`{"action":"set_field","advanced":{"name":"Width","value":"390","commit":"enter"}}`)
	require.True(t, r.IsError, r.Content)
	require.Contains(t, r.Content, `"valid":false`)
	require.NotContains(t, r.Content, `"verification":"verified"`)
}

func TestLiveRemountRejectsInvalidReplacement(t *testing.T) {
	l := openFigmaLike(t)
	l.eval(`document.body.innerHTML='<label>Width<input aria-label="Width" value="360"></label>';document.querySelector('input').addEventListener('keydown',e=>{if(e.key==='Enter'){const n=e.target.cloneNode(true);n.value='390';n.setAttribute('aria-invalid','true');e.target.replaceWith(n)}})`)
	r := l.run(`{"action":"set_field","advanced":{"name":"Width","value":"390","commit":"enter"}}`)
	require.True(t, r.IsError, r.Content)
	require.Contains(t, r.Content, `"valid":false`)
	require.NotContains(t, r.Content, `"verification":"verified"`)
}
