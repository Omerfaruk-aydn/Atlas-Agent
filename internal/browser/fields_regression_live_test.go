package browser

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// liveFrame is one entry of the current tab's frame tree.
type liveFrame struct{ ID, URL, LoaderID string }

// liveFrames maps each frame of fieldframes.html to its name query
// parameter ("A", "B", "inner") and the top document to "main".
func liveFrames(t *testing.T, s *chromedpSession) map[string]liveFrame {
	t.Helper()
	data, _, err := s.Advanced(t.Context(), Request{Action: "frames"})
	require.NoError(t, err)
	type node struct {
		Frame struct {
			ID       string `json:"id"`
			URL      string `json:"url"`
			LoaderID string `json:"loaderId"`
		} `json:"frame"`
		ChildFrames []json.RawMessage `json:"childFrames"`
	}
	var tree struct {
		FrameTree json.RawMessage `json:"frameTree"`
	}
	require.NoError(t, json.Unmarshal(data, &tree))
	out := map[string]liveFrame{}
	var walk func(raw json.RawMessage, top bool)
	walk = func(raw json.RawMessage, top bool) {
		var n node
		require.NoError(t, json.Unmarshal(raw, &n))
		name := "main"
		if !top {
			u, err := url.Parse(n.Frame.URL)
			require.NoError(t, err)
			name = u.Query().Get("name")
		}
		out[name] = liveFrame{ID: n.Frame.ID, URL: n.Frame.URL, LoaderID: n.Frame.LoaderID}
		for _, child := range n.ChildFrames {
			walk(child, false)
		}
	}
	walk(tree.FrameTree, true)
	return out
}

func evalString(t *testing.T, s *chromedpSession, expr string) string {
	t.Helper()
	raw, err := s.Eval(expr)
	require.NoError(t, err)
	var out string
	require.NoError(t, json.Unmarshal([]byte(raw), &out), raw)
	return out
}

// frameDoc is a top-document expression for a same-origin frame's document.
var frameDoc = map[string]string{
	"main":  "document",
	"A":     "document.getElementById('fa').contentDocument",
	"B":     "document.getElementById('fb').contentDocument",
	"inner": "document.getElementById('fa').contentDocument.querySelector('iframe').contentDocument",
}

func fieldIn(t *testing.T, s *chromedpSession, frame, expr string) string {
	t.Helper()
	return evalString(t, s, "String("+frameDoc[frame]+"."+expr+")")
}

func openFieldFrames(t *testing.T) (*chromedpSession, map[string]liveFrame) {
	t.Helper()
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/fieldframes.html"))
	require.Eventually(t, func() bool {
		raw, err := s.Eval("(()=>{try{return " + frameDoc["inner"] + ".getElementById('title').value}catch(e){return ''}})()")
		return err == nil && strings.Contains(raw, "frame inner")
	}, 10*time.Second, 50*time.Millisecond, "nested frames load")
	frames := liveFrames(t, s)
	for _, name := range []string{"main", "A", "B", "inner"} {
		require.NotEmpty(t, frames[name].ID, name)
	}
	return s, frames
}

func TestLiveFillFormPinsAllFramesBeforeWriting(t *testing.T) {
	s, frames := openFieldFrames(t)
	_, err := s.Eval(`document.getElementById('fa').contentDocument.getElementById('title').addEventListener('change',()=>{document.getElementById('fb').src='fieldframe.html?name=B2'},{once:true})`)
	require.NoError(t, err)
	result, err := s.FillForm(t.Context(), Request{Fields: []FormField{
		{FrameID: frames["A"].ID, Label: "Title", Value: "edited A", Commit: "blur"},
		{FrameID: frames["B"].ID, Label: "Title", Value: "must not be written", Commit: "blur"},
	}})
	require.ErrorContains(t, err, "stale_document")
	require.Len(t, result.Fields, 2)
	require.Equal(t, "not_attempted", result.Fields[1].Status)
	require.Equal(t, "frame B2", fieldIn(t, s, "B", "getElementById('title').value"))
	require.Equal(t, "edited A", fieldIn(t, s, "A", "getElementById('title').value"))
}

func TestLiveFillFormRejectsMissingFrameBeforeWriting(t *testing.T) {
	s, frames := openFieldFrames(t)
	before := titles(t, s)
	_, err := s.FillForm(t.Context(), Request{Fields: []FormField{
		{FrameID: frames["A"].ID, Label: "Title", Value: "must stay unchanged", Commit: "blur"},
		{FrameID: "missing-frame", Label: "Title", Value: "invalid", Commit: "blur"},
	}})
	require.ErrorContains(t, err, "stale_frame")
	require.Equal(t, before, titles(t, s), "preflight failure sends no input to any field")
}

func TestLiveFieldSelectionIdentityPersistsInFrame(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/fieldframes.html"))
	_, err := s.Eval(`(()=>{const f=document.getElementById('fb');f.src='panel.html?doc=identity&bw=100'})()`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		raw, err := s.Eval(`(()=>{try{return !!document.getElementById('fb').contentDocument.getElementById('w')}catch(e){return false}})()`)
		return err == nil && raw == "true"
	}, 5*time.Second, 50*time.Millisecond)
	_, err = s.Eval(`(()=>{const w=document.getElementById('fb').contentWindow;w.document.getElementById('opt-a').textContent='Rectangle';w.document.getElementById('opt-b').textContent='Rectangle';w.cards.b.h=50})()`)
	require.NoError(t, err)
	frame := liveFrames(t, s)[""]
	require.NotEmpty(t, frame.ID)
	req := Request{FrameID: frame.ID, Label: "Width", Within: "Design"}
	a, err := s.ReadField(t.Context(), req)
	require.NoError(t, err)
	again, err := s.ReadField(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, a.Identity, again.Identity, "reading the same isolated world preserves identity")
	_, err = s.Eval(`document.getElementById('fb').contentWindow.select('b')`)
	require.NoError(t, err)
	b, err := s.ReadField(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, a.Before, b.Before)
	require.Equal(t, "selection", a.Identity.Mismatch(b.Identity), "duplicate names inside frames still identify different objects")
}

func titles(t *testing.T, s *chromedpSession) map[string]string {
	out := map[string]string{}
	for _, name := range []string{"main", "A", "B", "inner"} {
		out[name] = fieldIn(t, s, name, "getElementById('title').value")
	}
	return out
}

// TestLiveFieldActionsStayInTheNamedFrame is the regression for set_field
// with a frame_id writing into the top document: every field operation must
// run in the named frame, and only there.
func TestLiveFieldActionsStayInTheNamedFrame(t *testing.T) {
	s, frames := openFieldFrames(t)
	ctx := t.Context()
	want := map[string]string{"main": "main", "A": "frame A", "B": "frame B", "inner": "frame inner"}
	require.Equal(t, want, titles(t, s))

	set := func(frame, label, value string) FieldResult {
		t.Helper()
		res, err := s.SetField(ctx, Request{FrameID: frames[frame].ID, Label: label, Value: value, Commit: "blur"})
		require.NoError(t, err, "%+v", res)
		require.Equal(t, FieldApplied, res.Status)
		require.Equal(t, frames[frame].ID, res.FrameID)
		require.Equal(t, frames[frame].LoaderID, res.DocumentID)
		return res
	}
	// Same label in the top document and in a frame: only the frame changes.
	set("A", "Title", "in A")
	want["A"] = "in A"
	require.Equal(t, want, titles(t, s))
	// Two sibling frames with the same field: only the named one changes.
	set("B", "Title", "in B")
	want["B"] = "in B"
	require.Equal(t, want, titles(t, s))
	// A frame nested in a frame.
	set("inner", "Title", "deep")
	want["inner"] = "deep"
	require.Equal(t, want, titles(t, s))
	// An open shadow root inside a frame.
	set("A", "Shadow note", "shadowed")
	require.Equal(t, "shadowed", fieldIn(t, s, "A", "getElementById('host').shadowRoot.getElementById('note').value"))
	require.Equal(t, "", fieldIn(t, s, "B", "getElementById('host').shadowRoot.getElementById('note').value"))

	// The isolated world used for frames leaves the application's own value
	// tracking intact: a controlled input sees the change and keeps it.
	set("A", "Tracked", "kept")
	require.Equal(t, "kept", evalString(t, s, "document.getElementById('fa').contentWindow.appState.tracked"))
	require.Equal(t, "", evalString(t, s, "document.getElementById('fb').contentWindow.appState.tracked"))

	// read_field reads the named frame's document.
	for frame, value := range map[string]string{"main": "main", "A": "in A", "B": "in B", "inner": "deep"} {
		req := Request{Label: "Title"}
		if frame != "main" {
			req.FrameID = frames[frame].ID
		}
		res, err := s.ReadField(ctx, req)
		require.NoError(t, err)
		require.Equal(t, value, res.Accepted, frame)
	}

	// select_option in one frame, by label and by option value.
	res, err := s.SelectOption(ctx, Request{FrameID: frames["B"].ID, Label: "Mode", Value: "Slow"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldApplied, res.Status)
	require.Equal(t, "s", res.AcceptedOption)
	require.Equal(t, "s", fieldIn(t, s, "B", "getElementById('mode').value"))
	require.Equal(t, "f", fieldIn(t, s, "A", "getElementById('mode').value"))
	require.Equal(t, "f", fieldIn(t, s, "main", "getElementById('mode').value"))
	res, err = s.SelectOption(ctx, Request{FrameID: frames["A"].ID, Label: "Mode", Value: "s"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldNormalized, res.Status, "an option value is the same choice as its label")
	require.Equal(t, "Slow", res.Accepted)

	// A custom combobox is chosen with real clicks; inside a frame the
	// click positions must include the frame offset.
	res, err = s.SelectOption(ctx, Request{FrameID: frames["B"].ID, Role: "combobox", Name: "Shape", Value: "Square"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, "Square", fieldIn(t, s, "B", "getElementById('shape').textContent"))
	require.Equal(t, "Circle", fieldIn(t, s, "A", "getElementById('shape').textContent"))

	// fill_form keeps the frame for each field.
	form, err := s.FillForm(ctx, Request{FrameID: frames["B"].ID, Fields: []FormField{{Label: "Title", Value: "form B", Commit: "blur"}}})
	require.NoError(t, err, "%+v", form)
	want["B"] = "form B"
	require.Equal(t, want, titles(t, s))
}

// TestLiveFieldFrameIdentityFailures checks that a frame that is wrong,
// navigated or gone fails explicitly and never falls back to the top
// document or another frame.
func TestLiveFieldFrameIdentityFailures(t *testing.T) {
	s, frames := openFieldFrames(t)
	ctx := t.Context()
	before := titles(t, s)

	_, err := s.SetField(ctx, Request{FrameID: "0123456789ABCDEF0123456789ABCDEF", Label: "Title", Value: "wrong", Commit: "blur", TimeoutMS: 3000})
	require.Error(t, err)
	require.Contains(t, err.Error(), "stale_frame")
	require.Equal(t, before, titles(t, s), "an unknown frame id changes nothing")
	_, err = s.ReadField(ctx, Request{FrameID: "0123456789ABCDEF0123456789ABCDEF", Label: "Title"})
	require.ErrorContains(t, err, "stale_frame")

	// Frame B navigates: a target pinned to its old document is refused.
	old := frames["B"]
	_, err = s.Eval("document.getElementById('fb').src = 'fieldframe.html?name=B2'")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		raw, err := s.Eval("(()=>{try{return " + frameDoc["B"] + ".getElementById('title').value}catch(e){return ''}})()")
		return err == nil && strings.Contains(raw, "frame B2")
	}, 5*time.Second, 50*time.Millisecond)
	_, err = s.SetField(ctx, Request{FrameID: old.ID, DocumentID: old.LoaderID, Label: "Title", Value: "stale", Commit: "blur", TimeoutMS: 3000})
	require.ErrorContains(t, err, "stale_document")
	require.Equal(t, "frame B2", fieldIn(t, s, "B", "getElementById('title').value"))
	_, err = s.ReadField(ctx, Request{FrameID: old.ID, DocumentID: old.LoaderID, Label: "Title"})
	require.ErrorContains(t, err, "stale_document")
	_, err = s.SelectOption(ctx, Request{FrameID: old.ID, DocumentID: old.LoaderID, Label: "Mode", Value: "Slow"})
	require.ErrorContains(t, err, "stale_document")
	require.Equal(t, "f", fieldIn(t, s, "B", "getElementById('mode').value"))

	// Frame B closes: its id no longer resolves and nothing else changes.
	_, err = s.Eval("document.getElementById('fb').remove()")
	require.NoError(t, err)
	_, err = s.SetField(ctx, Request{FrameID: old.ID, Label: "Title", Value: "gone", Commit: "blur", TimeoutMS: 3000})
	require.ErrorContains(t, err, "stale_frame")
	require.Equal(t, "main", fieldIn(t, s, "main", "getElementById('title').value"))
	require.Equal(t, before["A"], fieldIn(t, s, "A", "getElementById('title').value"))
}

// TestLiveCrossOriginFrameFields documents the cross-origin boundary: a
// frame from another site runs in its own renderer process and is not part
// of the tab's frame tree. Field actions refuse it with frame_unreachable
// and never fall back to the top document.
func TestLiveCrossOriginFrameFields(t *testing.T) {
	s, base := liveFixture(t)
	u, err := url.Parse(base)
	require.NoError(t, err)
	cross := "http://localhost:" + u.Port() + "/fieldframe.html?name=X"
	require.NoError(t, s.Navigate(base+"/fieldframes.html"))
	_, err = s.Eval("(()=>{const f=document.createElement('iframe');f.title='Cross';f.src=" + jsQuote(cross) + ";document.body.appendChild(f)})()")
	require.NoError(t, err)
	// Another site's frame is not part of the tab's frame tree; it is a
	// target of its own, which is how its id can be learned.
	var frame liveFrame
	require.Eventually(t, func() bool {
		if f := liveFrames(t, s)["X"]; f.ID != "" {
			frame = f
			return true
		}
		targets, err := s.command(s.currentContext(), "Target.getTargets", map[string]any{}, true)
		require.NoError(t, err)
		infos, _ := targets["targetInfos"].([]any)
		for _, item := range infos {
			info, _ := item.(map[string]any)
			if u, _ := info["url"].(string); info["type"] == "iframe" && strings.Contains(u, "name=X") {
				frame.ID, _ = info["targetId"].(string)
				return true
			}
		}
		return false
	}, 10*time.Second, 100*time.Millisecond)
	res, err := s.SetField(t.Context(), Request{FrameID: frame.ID, Label: "Title", Value: "cross", Commit: "blur", TimeoutMS: 5000})
	require.ErrorContains(t, err, "frame_unreachable", "%+v", res)
	require.Equal(t, "none", res.Dispatch)
	require.Equal(t, "main", fieldIn(t, s, "main", "getElementById('title').value"), "the top document never receives the write")
	_, err = s.ReadField(t.Context(), Request{FrameID: frame.ID, Label: "Title"})
	require.ErrorContains(t, err, "frame_unreachable")
	_, err = s.SelectOption(t.Context(), Request{FrameID: frame.ID, Label: "Mode", Value: "Slow"})
	require.ErrorContains(t, err, "frame_unreachable")
	require.Equal(t, "f", fieldIn(t, s, "main", "getElementById('mode').value"))
}

func jsQuote(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

// TestLiveFieldSettleWindow covers asynchronous acceptance and rejection,
// node replacement and reuse, unstable values, busy fields, cancellation
// and the cost for fields that answer at once.
func TestLiveFieldSettleWindow(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/async.html"))
	ctx := t.Context()

	// Rejected 800 ms after the commit: inside a 1500 ms window that is a
	// rejection, never an applied value.
	started := time.Now()
	res, err := s.SetField(ctx, Request{Label: "Rejected", Value: "new", Commit: "blur", SettleMS: 1500})
	require.ErrorIs(t, err, ErrFieldNotApplied, "%+v", res)
	require.Equal(t, FieldNotApplied, res.Status)
	require.Equal(t, "old", res.Accepted)
	require.GreaterOrEqual(t, time.Since(started), 1500*time.Millisecond, "the requested window is honored")
	require.GreaterOrEqual(t, res.ObservedMS, int64(1500))
	require.Equal(t, "old", fieldIn(t, s, "main", "getElementById('rejected').value"))

	// Cleared while processing, accepted 700 ms later.
	res, err = s.SetField(ctx, Request{Label: "Delayed", Value: "new", Commit: "blur", SettleMS: 1500})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldApplied, res.Status)
	require.Equal(t, "new", res.Accepted)
	require.Equal(t, "stable_window", res.Acceptance)

	// Committed on Enter, then replaced by a new node.
	res, err = s.SetField(ctx, Request{Label: "Remount", Value: "7", Commit: "enter"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldApplied, res.Status)
	require.True(t, res.NodeReplaced)
	require.Equal(t, "7", evalString(t, s, "state.remount"))

	// The same node is reused for another property during the readback:
	// identity is lost, so nothing is claimed.
	res, err = s.SetField(ctx, Request{Label: "Reuse", Value: "20", Commit: "blur", SettleMS: 600})
	require.Error(t, err)
	require.Contains(t, err.Error(), "outcome_unknown")
	require.Equal(t, FieldUnknown, res.Status)
	require.False(t, res.Succeeded())

	// aria-busy defers the answer until the application is done.
	started = time.Now()
	res, err = s.SetField(ctx, Request{Label: "Busy field", Value: "y", Commit: "blur"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldApplied, res.Status)
	require.GreaterOrEqual(t, time.Since(started), 600*time.Millisecond)

	// A field that answers at once does not wait for long.
	started = time.Now()
	res, err = s.SetField(ctx, Request{Label: "Fast", Value: "quick", Commit: "blur"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldApplied, res.Status)
	require.Less(t, time.Since(started), 1500*time.Millisecond)

	// commit none is never applied.
	res, err = s.SetField(ctx, Request{Label: "Fast", Value: "typed", Commit: "none"})
	require.NoError(t, err)
	require.Equal(t, FieldTyped, res.Status)
	require.False(t, res.Succeeded())

	// append and replace_selection are checked against the whole field.
	res, err = s.SetField(ctx, Request{Label: "Tags", Value: ",c", Mode: "append", Commit: "blur"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldApplied, res.Status)
	require.Equal(t, "a,b,c", res.Accepted)
	_, err = s.Eval("document.getElementById('phrase').setSelectionRange(6, 11)")
	require.NoError(t, err)
	res, err = s.SetField(ctx, Request{Label: "Phrase", Value: "there", Mode: "replace_selection", Commit: "blur"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldApplied, res.Status)
	require.Equal(t, "hello there", res.Accepted)

	// A value that never settles is not reported as applied.
	started = time.Now()
	res, err = s.SetField(ctx, Request{Label: "Flutter", Value: "z", Commit: "blur", SettleMS: 300})
	require.Error(t, err)
	require.Contains(t, err.Error(), "value_unstable")
	require.Equal(t, FieldUnknown, res.Status)
	require.Less(t, time.Since(started), 5*time.Second, "the window is bounded")

	// Timeout and cancellation end the wait without a claim.
	started = time.Now()
	res, err = s.SetField(ctx, Request{Label: "Flutter", Value: "y", Commit: "blur", SettleMS: 2000, TimeoutMS: 700})
	require.Error(t, err)
	require.False(t, res.Succeeded())
	require.Less(t, time.Since(started), 2*time.Second)
	canceled, cancel := context.WithCancel(ctx)
	time.AfterFunc(300*time.Millisecond, cancel)
	started = time.Now()
	res, err = s.SetField(canceled, Request{Label: "Flutter", Value: "x", Commit: "blur", SettleMS: 3000})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, res.Succeeded())
	require.Less(t, time.Since(started), 1500*time.Millisecond)

	// Native select: label and option value are told apart.
	res, err = s.SelectOption(ctx, Request{Label: "Size", Value: "Large"})
	require.NoError(t, err)
	require.Equal(t, FieldApplied, res.Status)
	require.Equal(t, "l", res.AcceptedOption)
	res, err = s.SelectOption(ctx, Request{Label: "Size", Value: "s"})
	require.NoError(t, err)
	require.Equal(t, FieldNormalized, res.Status)
	require.Equal(t, "Small", res.Accepted)
}

// TestLiveOnlyInvalidDoesNotSkipDifferentValues is the fill_form side of
// the comparison fix: a field holding 100% is not "already" 100px, and a
// field holding secreta is not already SecretA.
func TestLiveOnlyInvalidDoesNotSkipDifferentValues(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/async.html"))
	res, err := s.FillForm(t.Context(), Request{OnlyInvalid: true, Fields: []FormField{
		{Label: "Spacing", Value: "100px", Commit: "blur"},
		{Label: "Code", Value: "SecretA", Commit: "blur"},
		{Label: "Tags", Value: "a,b", Commit: "blur"},
	}})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, 2, res.Applied)
	require.Equal(t, 1, res.Skipped, "only the field that already holds exactly its value is skipped")
	require.Equal(t, "100px", fieldIn(t, s, "main", "getElementById('spacing').value"))
	require.Equal(t, "SecretA", fieldIn(t, s, "main", "getElementById('code').value"))
}
