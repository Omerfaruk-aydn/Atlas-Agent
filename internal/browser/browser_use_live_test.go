package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// liveFixture starts a headless browser against the local fixture pages.
// These tests use only local pages and never a real account; they exercise
// generic browser mechanics and do not prove compatibility with any site.
func liveFixture(t *testing.T) (*chromedpSession, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("Real browser excluded in short mode")
	}
	exe := findChrome()
	if exe == "" {
		// A skip is not browser proof; ATLAS_REQUIRE_BROWSER turns it into
		// a failure for runs that must exercise a real browser.
		if os.Getenv("ATLAS_REQUIRE_BROWSER") != "" {
			t.Fatal("ATLAS_REQUIRE_BROWSER is set but no browser was found")
		}
		t.Skip("No installed browser")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.Handle("/", http.FileServer(http.Dir("testdata/browseruse")))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	sess, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), OverlayDisabled: true, ActionTimeout: 15 * time.Second})
	require.NoError(t, err)
	t.Cleanup(sess.Close)
	return sess.(*chromedpSession), server.URL
}

func evalJSON(t *testing.T, s *chromedpSession, expr string, out any) {
	t.Helper()
	raw, err := s.Eval(expr)
	require.NoError(t, err)
	var text string
	require.NoError(t, json.Unmarshal([]byte(raw), &text))
	require.NoError(t, json.Unmarshal([]byte(text), out))
}

func findElement(elements []SnapshotElement, match func(SnapshotElement) bool) (SnapshotElement, bool) {
	for _, el := range elements {
		if match(el) {
			return el, true
		}
	}
	return SnapshotElement{}, false
}

func TestLiveObservationContextHintsAndDocumentIdentity(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/design.html"))
	obs, err := s.Observe(t.Context(), ObserveRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, obs.Document)
	require.NotEmpty(t, obs.TabID)

	radius, ok := findElement(obs.Elements, func(e SnapshotElement) bool { return e.Tag == "input" && e.Name == "" })
	require.True(t, ok, "the unnamed radius field is observed")
	require.Equal(t, "Corner radius", radius.Hint, "an unnamed field carries a nearby label as its hint")
	require.Contains(t, radius.Context, `region "Appearance"`)

	closes := 0
	for _, el := range obs.Elements {
		if el.Name == "Close" {
			closes++
			require.NotEmpty(t, el.Context, "same-named buttons are told apart by their container")
		}
	}
	require.Equal(t, 2, closes)

	scoped, err := s.Observe(t.Context(), ObserveRequest{Scope: "#panel"})
	require.NoError(t, err)
	require.Less(t, len(scoped.Elements), len(obs.Elements))

	require.NoError(t, s.Navigate(base+"/form.html"))
	next, err := s.Observe(t.Context(), ObserveRequest{})
	require.NoError(t, err)
	require.NotEqual(t, obs.Document, next.Document, "navigation replaces the document identity")
}

func TestLiveSetFieldSeparatesTypedFromApplied(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/design.html"))
	ctx := t.Context()

	// Width commits on Enter after 200 ms; readback must wait for it.
	res, err := s.SetField(ctx, Request{Role: "textbox", Name: "Width", Value: "390"})
	require.NoError(t, err)
	require.Equal(t, FieldApplied, res.Status)
	require.Equal(t, "enter+blur", res.Commit, "an unchanged focused echo is finalized by releasing focus")
	var model struct {
		W, H, Radius float64
		Fill         string
		Commits      []string
	}
	evalJSON(t, s, "JSON.stringify(window.model)", &model)
	require.Equal(t, 390.0, model.W)

	// Height clamps to 2000: the application changed the value.
	res, err = s.SetField(ctx, Request{Role: "textbox", Name: "Height", Value: "5000"})
	require.NoError(t, err)
	require.Equal(t, FieldAdjusted, res.Status)
	require.Equal(t, "2000", res.Accepted)
	require.Equal(t, "differs", res.Verification)

	// Regression for the corner-radius report: an unnamed field resolved by
	// its nearby label, committed asynchronously on a remounted node.
	res, err = s.SetField(ctx, Request{Label: "Corner radius", Value: "12"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldApplied, res.Status)
	require.True(t, res.NodeReplaced, "the committed value lives on a new node")
	evalJSON(t, s, "JSON.stringify(window.model)", &model)
	require.Equal(t, 12.0, model.Radius)

	// A value the application rejects keeps the old value: never success.
	res, err = s.SetField(ctx, Request{Label: "Corner radius", Value: "abc"})
	require.ErrorIs(t, err, ErrFieldNotApplied)
	require.Equal(t, FieldNotApplied, res.Status)
	require.Equal(t, "12", res.Accepted)
	require.Equal(t, "abc", res.Typed, "the typed echo is reported separately from the accepted value")

	// Fill commits on change; the application reformats the value, which
	// counts as normalized rather than as a different value.
	res, err = s.SetField(ctx, Request{Role: "textbox", Name: "Fill color", Value: "f7f3ea"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, FieldNormalized, res.Status)
	require.Equal(t, "F7F3EA", res.Accepted, "the application's own formatting is reported")
	evalJSON(t, s, "JSON.stringify(window.model)", &model)
	require.Equal(t, "#f7f3ea", model.Fill)

	// A field that commits only on blur: Enter leaves the typed echo, focus
	// is released, and only then is the value applied.
	res, err = s.SetField(ctx, Request{Label: "Opacity", Value: "50"})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, "enter+blur", res.Commit)
	var opacity struct{ Opacity float64 }
	evalJSON(t, s, "JSON.stringify(window.model)", &opacity)
	require.Equal(t, 50.0, opacity.Opacity)

	// Commit none never claims the value was applied.
	res, err = s.SetField(ctx, Request{Role: "textbox", Name: "Width", Value: "400", Commit: "none"})
	require.NoError(t, err)
	require.Equal(t, FieldTyped, res.Status)
	require.Equal(t, "not_committed", res.Verification)
}

func TestLiveScopedTargetsSeparateSameNamedControls(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/design.html"))
	ctx := t.Context()

	_, _, err := s.Advanced(ctx, Request{Action: "semantic_click", Role: "button", Name: "Close", TimeoutMS: 2000})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous_target")
	require.Contains(t, err.Error(), "Layers", "candidates carry their container so the model can refine")

	// Dismissing the notification must not close the Layers panel.
	data, _, err := s.Advanced(ctx, Request{Action: "semantic_click", Role: "button", Name: "Dismiss", Within: "Notification", TimeoutMS: 2000})
	require.NoError(t, err)
	require.Contains(t, string(data), "Notification")
	_, _, err = s.Advanced(ctx, Request{Action: "assert", Selector: "#toast", Condition: "count", Expected: "0", TimeoutMS: 2000})
	require.NoError(t, err)
	_, _, err = s.Advanced(ctx, Request{Action: "assert", Selector: "section[aria-label=Layers]", Condition: "count", Expected: "1", TimeoutMS: 2000})
	require.NoError(t, err)

	_, _, err = s.Advanced(ctx, Request{Action: "semantic_click", Role: "button", Name: "Close", Within: "Layers", TimeoutMS: 2000})
	require.NoError(t, err)
	_, _, err = s.Advanced(ctx, Request{Action: "wait_for", Selector: "section[aria-label=Layers]", Condition: "count", Expected: "0", TimeoutMS: 2000})
	require.NoError(t, err)
	_, _, err = s.Advanced(ctx, Request{Action: "assert", Selector: "#export", Condition: "count", Expected: "1", TimeoutMS: 2000})
	require.NoError(t, err, "the other panel's Close was not used")
}

func TestLiveFillFormFixesOnlyInvalidFields(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/form.html"))
	ctx := t.Context()
	fields := []FormField{
		{Label: "Email", Value: "not-an-email"},
		{Label: "Age", Value: "30"},
		{Label: "Country", Value: "Türkiye", Kind: "option"},
		{Label: "Start", Value: "2026-11-02"},
		{Label: "Accept terms", Value: "true"},
		{Role: "combobox", Name: "Plan", Value: "Pro", Kind: "option"},
	}
	res, err := s.FillForm(ctx, Request{Fields: fields})
	require.ErrorIs(t, err, ErrFieldNotApplied, "%+v", res)
	require.Equal(t, 5, res.Applied)
	require.Equal(t, 1, res.Failed, "a retained but invalid value is not verified as applied")
	require.Equal(t, "failed", res.Fields[0].Field.Verification)
	require.Equal(t, 1, res.Invalid, "the bad email is reported by the page's own validation")
	require.NotEmpty(t, res.Fields[0].Message)

	fields[0].Value = "user@example.com"
	res, err = s.FillForm(ctx, Request{Fields: fields, OnlyInvalid: true})
	require.NoError(t, err, "%+v", res)
	require.Equal(t, 1, res.Applied, "only the invalid field changes")
	require.Equal(t, 5, res.Skipped)
	require.Zero(t, res.Invalid)
	var submissions int
	evalJSON(t, s, "JSON.stringify(window.submissions)", &submissions)
	require.Zero(t, submissions, "filling never submits")
}

func TestLiveCollectVirtualizedListWithPagination(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/list.html"))
	ctx := t.Context()

	partial, err := s.Collect(ctx, Request{RowSelector: "[role=row]", Scope: "#viewport", MaxItems: 60})
	require.NoError(t, err)
	require.False(t, partial.Complete)
	require.Equal(t, "max_items", partial.StopReason)
	require.Contains(t, partial.Next, "incomplete")

	full, err := s.Collect(ctx, Request{RowSelector: "[role=row]", Scope: "#viewport", NextSelector: "#next", MaxItems: 1000, MaxScrolls: 200})
	require.NoError(t, err)
	require.Equal(t, 500, full.Count, "both pages, every row once: %s", full.StopReason)
	require.Equal(t, 2, full.Pages)
	require.True(t, full.EndReached)
	require.Empty(t, full.Gaps)
	require.True(t, full.Complete)
	keys := map[string]bool{}
	for _, item := range full.Items {
		require.False(t, keys[item.Key], "row identity is unique: %s", item.Key)
		keys[item.Key] = true
	}
}

func TestLiveFramesAndShadowTargets(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/frames.html"))
	ctx := t.Context()
	_, _, err := s.Advanced(ctx, Request{Action: "wait_for", Condition: "ready", TimeoutMS: 5000})
	require.NoError(t, err)

	obs, err := s.Observe(ctx, ObserveRequest{})
	require.NoError(t, err)
	nick, ok := findElement(obs.Elements, func(e SnapshotElement) bool { return e.Name == "Nickname" })
	require.True(t, ok)
	require.True(t, nick.Shadow)
	_, ok = findElement(obs.Elements, func(e SnapshotElement) bool { return e.Role == "iframe" && e.Name == "Outer frame" })
	require.True(t, ok, "frames are listed so the model looks inside them instead of guessing coordinates")

	res, err := s.SetField(ctx, Request{Label: "Nickname", Value: "Atlas", Commit: "blur"})
	require.NoError(t, err, "%+v", res)
	_, _, err = s.Advanced(ctx, Request{Action: "semantic_click", Role: "button", Name: "Apply", Within: "Shadow settings"})
	require.NoError(t, err)
	_, _, err = s.Advanced(ctx, Request{Action: "wait_for", Selector: "#status", Condition: "text", Expected: "Applied Atlas", TimeoutMS: 2000})
	require.NoError(t, err)

	tree, _, err := s.Advanced(ctx, Request{Action: "frames"})
	require.NoError(t, err)
	var frames struct {
		FrameTree struct {
			ChildFrames []struct {
				Frame       struct{ ID string }
				ChildFrames []struct{ Frame struct{ ID string } }
			}
		}
	}
	require.NoError(t, json.Unmarshal(tree, &frames))
	require.Len(t, frames.FrameTree.ChildFrames, 1)
	require.Len(t, frames.FrameTree.ChildFrames[0].ChildFrames, 1)
	inner := frames.FrameTree.ChildFrames[0].ChildFrames[0].Frame.ID
	_, _, err = s.Advanced(ctx, Request{Action: "semantic_click", Role: "button", Name: "Deep action", FrameID: inner})
	require.NoError(t, err)
	_, _, err = s.Advanced(ctx, Request{Action: "wait_for", Selector: "#deep", Condition: "text", Expected: "Pressed", FrameID: inner, TimeoutMS: 2000})
	require.NoError(t, err)
}

func TestLivePopupThenReturnToWorkTab(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/popup.html"))
	ctx := t.Context()
	_, _, err := s.Advanced(ctx, Request{Action: "tabs"})
	require.NoError(t, err)
	work := s.ActiveTab()
	_, _, err = s.Advanced(ctx, Request{Action: "semantic_click", Role: "button", Name: "Sign in with Provider"})
	require.NoError(t, err)
	data, _, err := s.Advanced(ctx, Request{Action: "popup_wait", Purpose: "provider sign-in", TimeoutMS: 5000})
	require.NoError(t, err)
	var popup struct {
		TabID  string `json:"tab_id"`
		Opener string `json:"opener_id"`
	}
	require.NoError(t, json.Unmarshal(data, &popup))
	require.Equal(t, work, popup.Opener)
	_, _, err = s.Advanced(ctx, Request{Action: "tab_select", TabID: popup.TabID})
	require.NoError(t, err)
	_, _, err = s.Advanced(ctx, Request{Action: "semantic_click", Role: "button", Name: "Allow", TimeoutMS: 5000})
	if err != nil {
		// Closing the target can discard the click acknowledgment. The
		// closure and opener-side effect are independently required below.
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}

	// The popup closed itself. Page actions report tab_closed, the browser
	// survives, and tab_return goes back to the opener.
	require.Eventually(t, s.tabClosed, 5*time.Second, 50*time.Millisecond)
	require.False(t, s.disconnected(), "a closed popup is not a lost browser")
	_, _, err = s.Advanced(ctx, Request{Action: "find", Role: "button"})
	require.ErrorIs(t, err, ErrTabClosed)
	_, _, err = s.Advanced(ctx, Request{Action: "tab_return"})
	require.NoError(t, err)
	require.Equal(t, work, s.ActiveTab())
	_, _, err = s.Advanced(ctx, Request{Action: "wait_for", Selector: "#state", Condition: "text", Expected: "Signed in", TimeoutMS: 3000})
	require.NoError(t, err)

	listing, _, err := s.Advanced(ctx, Request{Action: "tabs"})
	require.NoError(t, err)
	require.Contains(t, string(listing), `"owned":true`)
}

func TestLiveReadinessUnderPersistentTraffic(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/busy.html"))
	ctx := t.Context()
	_, _, err := s.Advanced(ctx, Request{Action: "wait_for", Condition: "network_idle", TimeoutMS: 700})
	require.Error(t, err)
	require.Contains(t, err.Error(), "pending_requests", "the timeout says what it last saw")

	started := time.Now()
	_, _, err = s.Advanced(ctx, Request{Action: "wait_for", Selector: "#status", Condition: "not_busy", TimeoutMS: 5000})
	require.NoError(t, err)
	require.Less(t, time.Since(started), 3*time.Second, "a satisfied condition returns without waiting out the timeout")

	_, _, err = s.Advanced(ctx, Request{Action: "wait_for", Selector: "#status", Condition: "text", Expected: "Never", TimeoutMS: 300})
	require.Error(t, err)
	require.Contains(t, err.Error(), `"actual":"Ready"`)
}

func TestLiveEvalTimeoutIsBounded(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/busy.html"))
	started := time.Now()
	_, err := s.EvalContext(t.Context(), "new Promise(() => {})", 400*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "eval_timeout")
	require.Less(t, time.Since(started), 3*time.Second)

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.EvalContext(canceled, "1", time.Second)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestLiveStaleViewportAfterScroll(t *testing.T) {
	s, base := liveFixture(t)
	require.NoError(t, s.Navigate(base+"/list.html"))
	_, err := s.Eval("document.body.style.height='3000px'")
	require.NoError(t, err)
	_, vp, err := s.ViewportScreenshot(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, vp.ID)
	require.NoError(t, s.Scroll(0, 400))
	_, err = s.Input(t.Context(), Request{Action: InputPointerClick, ViewportID: vp.ID, X: 10, Y: 10})
	require.ErrorIs(t, err, ErrStaleViewport)

	_, err = s.Input(t.Context(), Request{Action: InputKeyCombo, Key: "f"})
	require.NoError(t, err, "single letters are ordinary keys")
	_, _, err = NormalizeKey("hyper+f")
	require.True(t, strings.Contains(err.Error(), "unsupported_key"))
}
