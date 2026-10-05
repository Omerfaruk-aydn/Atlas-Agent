package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivityDoesNotPolluteInputExtractionOrCapture(t *testing.T) {
	exe := ""
	for _, p := range []string{"C:/Program Files/Google/Chrome/Application/chrome.exe", "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "/usr/bin/chromium", "/usr/bin/google-chrome"} {
		if _, err := os.Stat(p); err == nil {
			exe = p
			break
		}
	}
	if exe == "" {
		t.Skip("Chrome/Chromium not installed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "img-src 'self'; style-src 'unsafe-inline'")
		_, _ = w.Write([]byte(`<html><body><input id="name"><button id="save" onclick="this.textContent='Saved'">Save</button><iframe hidden srcdoc="<input>"></iframe></body></html>`))
	}))
	defer server.Close()
	session, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), ActionTimeout: 10 * time.Second})
	require.NoError(t, err)
	defer session.Close()
	s := session.(*chromedpSession)
	s.activityEnabled = true
	// Frame feedback assertions require the normal-motion presentation mode.
	require.NoError(t, s.run(emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}})))
	if os.Getenv("ATLAS_ACTIVITY_4K") == "1" {
		require.NoError(t, s.run(emulation.SetDeviceMetricsOverride(1920, 1080, 2, false)))
	}
	require.NoError(t, s.Navigate(server.URL))
	baseline, err := s.Screenshot(false)
	require.NoError(t, err)
	finish := s.BeginActivity(t.Context(), "fixture", "click", "#save")
	require.Eventually(t, func() bool {
		v, _ := s.Eval(`document.querySelectorAll('[data-atlas-activity]').length`)
		return v == "1"
	}, time.Second, 20*time.Millisecond)
	visible, err := s.Eval(`document.querySelectorAll('[data-atlas-activity]').length`)
	require.NoError(t, err)
	require.Equal(t, "1", visible)
	edges, err := s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].edges.querySelectorAll('mask .feather').length`)
	require.NoError(t, err)
	require.Equal(t, "4", edges)
	radius, err := s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].edges.querySelector('.rounded-frame').getAttribute('rx')`)
	require.NoError(t, err)
	require.Equal(t, `"17.5"`, radius)
	caption, err := s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].edges.parentNode.querySelector('.control-banner')?.textContent`)
	require.NoError(t, err)
	require.Contains(t, caption, "Atlas is using your browser")
	strip, err := s.Eval(`(()=>{const b=window[Symbol.for('atlas.agent.activity.v1')].edges.parentNode.querySelector('.control-banner');return {height:b.getBoundingClientRect().height,radius:getComputedStyle(b).borderRadius,pointer:!!b.querySelector('.banner-icon svg')}})()`)
	require.NoError(t, err)
	require.JSONEq(t, `{"height":38,"radius":"6px","pointer":true}`, strip)
	s.updateActivityPoint(300, 200)
	// Include asynchronous delivery and frame scheduling on loaded CI runners.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		state, err := s.Eval(`(()=>{const a=window[Symbol.for('atlas.agent.activity.v1')];return {x:a.x,y:a.y,displayX:a.displayX,displayY:a.displayY}})()`)
		require.NoError(c, err)
		require.JSONEq(c, `{"x":300,"y":200,"displayX":300,"displayY":200}`, state)
	}, 3*time.Second, 20*time.Millisecond)
	var preview []byte
	require.NoError(t, s.run(chromedp.CaptureScreenshot(&preview)))
	before, err := png.Decode(bytes.NewReader(baseline))
	require.NoError(t, err)
	after, err := png.Decode(bytes.NewReader(preview))
	require.NoError(t, err)
	// Verify actual edge pixels under CSP, independently of the DOM mask.
	x := after.Bounds().Dx() / 2
	require.NotEqual(t, before.At(x, 0), after.At(x, 0))
	require.Equal(t, before.At(0, 0), after.At(0, 0), "The frame's outer corner is rounded away")
	depth := 30
	if os.Getenv("ATLAS_ACTIVITY_4K") == "1" {
		depth *= 2
	}
	r, g, b, _ := after.At(after.Bounds().Dx()/10, depth).RGBA()
	require.Greater(t, max(r, g, b)-min(r, g, b), uint32(3000), "RGB smoke is visible well inside the rim")
	// Inspect the actual arrow fill, away from page controls and edge smoke.
	px, py := 305, 205
	if os.Getenv("ATLAS_ACTIVITY_4K") == "1" {
		px, py = px*2, py*2
	}
	r, g, b, _ = after.At(px, py).RGBA()
	require.Less(t, max(r, g, b), uint32(140*257), "The replacement cursor must paint visible dark pixels")
	if path := os.Getenv("ATLAS_ACTIVITY_PREVIEW"); path != "" {
		require.NoError(t, os.WriteFile(path, preview, 0o600))
	}
	_, region, err := s.Advanced(t.Context(), Request{Action: "capture_region", X: 0, Y: 0, Width: 300, Height: 200})
	require.NoError(t, err)
	require.NotEmpty(t, region)
	activity.ClearSession("fixture")
	finish()
	require.Eventually(t, func() bool {
		v, _ := s.Eval(`document.querySelectorAll('[data-atlas-activity]').length`)
		return v == "0"
	}, time.Second, 20*time.Millisecond)
	finish = s.BeginActivity(t.Context(), "fixture", "click", "#save")
	text, err := s.Text("body")
	require.NoError(t, err)
	require.NotContains(t, text, "ATLAS")
	html, err := s.HTML("html")
	require.NoError(t, err)
	require.NotContains(t, html, "data-atlas-activity")
	image, err := s.Screenshot(false)
	require.NoError(t, err)
	require.Equal(t, baseline, image)
	raw, err := s.RawCDP("Page.captureScreenshot", map[string]any{"format": "png"})
	require.NoError(t, err)
	rawImage, err := base64.StdEncoding.DecodeString(raw["data"].(string))
	require.NoError(t, err)
	require.Equal(t, baseline, rawImage)
	require.NoError(t, s.Click("#save"))
	finish()
	text, err = s.Text("#save")
	require.NoError(t, err)
	require.Equal(t, "Saved", text)
	require.NoError(t, s.Navigate(server.URL))
	flowCtx, endFlow := activity.StartFlow(t.Context(), "fixture")
	defer endFlow()
	finish = s.BeginActivity(flowCtx, "fixture", "type", "#name")
	require.NoError(t, s.Type("#name", "fixture"))
	finish()
	wait := 650 * time.Millisecond
	if os.Getenv("ATLAS_ACTIVITY_LONG_WAIT") == "1" {
		wait = 31 * time.Second
	}
	time.Sleep(wait)
	countWhileWaiting, err := s.Eval(`document.querySelectorAll('[data-atlas-activity]').length`)
	require.NoError(t, err)
	require.Equal(t, "1", countWhileWaiting)
	_, err = s.Eval(`window.pointerSamples=[];window.samplePointer=true;(()=>{const frame=()=>{const a=window[Symbol.for('atlas.agent.activity.v1')];const ring=a.edges.parentNode.querySelector('.cursor-click-ring');window.pointerSamples.push({x:a.displayX,y:a.displayY,ring:Number(ring.getAttribute('opacity'))});if(window.samplePointer)requestAnimationFrame(frame)};requestAnimationFrame(frame)})();document.addEventListener('click',e=>{const a=window[Symbol.for('atlas.agent.activity.v1')];window.pointerAtClick={x:a.displayX,y:a.displayY,targetX:e.clientX,targetY:e.clientY}})`)
	require.NoError(t, err)
	require.NoError(t, s.ClickAt(420, 300))
	atClick, err := s.Eval(`window.pointerAtClick`)
	require.NoError(t, err)
	require.JSONEq(t, `{"x":420,"y":300,"targetX":420,"targetY":300}`, atClick)
	paintedSteps, err := s.Eval(`window.pointerSamples.filter(p=>p.x>50&&p.x<419).length`)
	require.NoError(t, err)
	require.NotEqual(t, "0", paintedSteps, "The real frame loop must show movement before the DOM click")
	// Record feedback on animation frames rather than sampling after CDP
	// round trips, which can outlast the brief cue on a loaded runner.
	require.Eventually(t, func() bool {
		v, _ := s.Eval(`window.pointerSamples.some(p=>p.ring>0)`)
		return v == "true"
	}, 2*time.Second, 20*time.Millisecond)
	_, err = s.Eval(`window.samplePointer=false`)
	require.NoError(t, err)
	require.NoError(t, s.ClickAt(420.25, 300.75))
	atClick, err = s.Eval(`window.pointerAtClick`)
	require.NoError(t, err)
	require.JSONEq(t, `{"x":420.25,"y":300.75,"targetX":420,"targetY":300}`, atClick)
	require.Equal(t, "click", s.getActivityManager().Snapshot().PointerKind)
	require.Eventually(t, func() bool {
		v, _ := s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].edges.parentNode.querySelector('.cursor').style.getPropertyValue('--pointer-scale')`)
		return v == `"1"`
	}, time.Second, 20*time.Millisecond)
	for _, params := range []map[string]any{
		{"type": "mousePressed", "x": 450, "y": 310, "button": "left", "buttons": 1, "clickCount": 1},
		{"type": "mouseMoved", "x": 480, "y": 320, "button": "left", "buttons": 1},
	} {
		finish = s.BeginActivity(flowCtx, "fixture", "cdp", "")
		_, err = s.RawCDP("Input.dispatchMouseEvent", params)
		require.NoError(t, err)
		finish()
		s.getActivityManager().Present()
		require.Contains(t, []string{"press", "drag_move"}, s.getActivityManager().Snapshot().PointerKind)
	}
	held, err := s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].edges.parentNode.querySelector('.cursor').dataset.held`)
	require.NoError(t, err)
	require.Equal(t, `"true"`, held)
	finish = s.BeginActivity(flowCtx, "fixture", "cdp", "")
	_, err = s.RawCDP("Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": 480, "y": 320, "button": "left", "buttons": 0, "clickCount": 1})
	require.NoError(t, err)
	require.Equal(t, "release", s.getActivityManager().Snapshot().PointerKind)
	finish()
	s.getActivityManager().Present()
	held, err = s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].edges.parentNode.querySelector('.cursor').dataset.held`)
	require.NoError(t, err)
	require.Equal(t, `"false"`, held)
	cursor, err := s.Eval(`getComputedStyle(document.querySelector('#name')).cursor`)
	require.NoError(t, err)
	require.Equal(t, `"none"`, cursor)
	_, err = s.Eval(`window.activityFrameStart=window[Symbol.for('atlas.agent.activity.v1')].frames;window.activityFrameTime=performance.now()`)
	require.NoError(t, err)
	time.Sleep(2 * time.Second)
	fps, err := s.Eval(`(window[Symbol.for('atlas.agent.activity.v1')].frames-window.activityFrameStart)/((performance.now()-window.activityFrameTime)/1000)`)
	require.NoError(t, err)
	t.Logf("Browser RGB edge animation: %s frames/s", fps)
	require.NoError(t, s.run(emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}})))
	require.Eventually(t, func() bool {
		v, _ := s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].edges.style.filter`)
		return v == `"none"`
	}, time.Second, 20*time.Millisecond)
	require.NoError(t, s.run(emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}})))
	s.activityReducedMotion = true
	finish = s.BeginActivity(flowCtx, "fixture", "observe", "")
	finish()
	require.Eventually(t, func() bool {
		v, _ := s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].edges.style.filter`)
		return v == `"none"`
	}, time.Second, 20*time.Millisecond)
	_, err = s.Eval(`window.activityRemovals=0;window.activityObserver=new MutationObserver(rs=>{for(const r of rs)for(const n of r.removedNodes)if(n===window[Symbol.for('atlas.agent.activity.v1')].host)window.activityRemovals++});window.activityObserver.observe(document.documentElement,{childList:true})`)
	require.NoError(t, err)
	_, err = s.Screenshot(false)
	require.NoError(t, err)
	html, err = s.HTML("html")
	require.NoError(t, err)
	require.NotContains(t, html, "data-atlas-activity")
	_, err = s.Snapshot(false)
	require.NoError(t, err)
	removals, err := s.Eval(`window.activityRemovals`)
	require.NoError(t, err)
	require.Equal(t, "0", removals)
	endFlow()
	require.Eventually(t, func() bool {
		v, _ := s.Eval(`document.querySelectorAll('[data-atlas-activity]').length`)
		return v == "0"
	}, time.Second, 20*time.Millisecond)
	cursor, err = s.Eval(`getComputedStyle(document.querySelector('#name')).cursor`)
	require.NoError(t, err)
	require.NotEqual(t, `"none"`, cursor)
	if runtime.GOOS == "windows" {
		stopCtx, finishStopFlow := activity.StartFlow(activity.WithLanguage(t.Context(), "tr"), "stop-banner-fixture")
		defer finishStopFlow()
		s.BeginActivity(stopCtx, "stop-banner-fixture", "observe", "")()
		require.NoError(t, s.PressKey("escape"))
		require.Eventually(t, func() bool {
			v, _ := s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].canStop`)
			return v == "true"
		}, time.Second, 20*time.Millisecond)
		if path := os.Getenv("ATLAS_CONTROL_BANNER_PREVIEW"); path != "" {
			var image []byte
			require.NoError(t, s.run(chromedp.CaptureScreenshot(&image)))
			require.NoError(t, os.WriteFile(path, image, 0o600))
		}
		require.NoError(t, s.run(emulation.SetDeviceMetricsOverride(320, 240, 2, false)))
		compact, err := s.Eval(`(()=>{const b=window[Symbol.for('atlas.agent.activity.v1')].edges.parentNode.querySelector('.control-banner'),c=b.querySelector('.caption'),h=b.querySelector('.stop-hint'),r=b.getBoundingClientRect();return {fits:r.left>=16&&r.right<=innerWidth-16,height:r.height,separate:c.getBoundingClientRect().right<=h.getBoundingClientRect().left,stop:b.querySelector('.stop-label').textContent,ellipsis:c.scrollWidth>c.clientWidth}})()`)
		require.NoError(t, err)
		require.JSONEq(t, `{"fits":true,"height":38,"separate":true,"stop":"Durdur","ellipsis":true}`, compact)
		if os.Getenv("ATLAS_ACTIVITY_4K") == "1" {
			require.NoError(t, s.run(emulation.SetDeviceMetricsOverride(1920, 1080, 2, false)))
		} else {
			require.NoError(t, s.run(emulation.SetDeviceMetricsOverride(800, 600, 1, false)))
		}
		// Agent Escape and page-created keyboard events must not cancel the run.
		require.NoError(t, s.PressKey("escape"))
		_, err = s.Eval(`window.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))`)
		require.NoError(t, err)
		require.NoError(t, stopCtx.Err())
		// CDP key events are trusted DOM events, but they are not physical input.
		require.NoError(t, s.run(chromedp.KeyEvent(kb.Escape)))
		_, err = s.RawCDP("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
		require.NoError(t, err)
		_, err = s.RawCDP("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
		require.NoError(t, err)
		require.NoError(t, stopCtx.Err())
		binding, err := s.Eval(`typeof window.__atlasStopControl`)
		require.NoError(t, err)
		require.Equal(t, `"undefined"`, binding, "Page code must have no cancellation authority")
		_, err = s.Eval(`(()=>{const f=document.querySelector('iframe');f.hidden=false;f.contentDocument.querySelector('input').focus()})()`)
		require.NoError(t, err)
		// The native input callback sees page focus even within an embedded form.
		_, err = s.Eval(`document.hasFocus=()=>false;Document.prototype.hasFocus=()=>false`)
		require.NoError(t, err)
		s.handleActivityEscape(s.getActivityManager().StopRevision())
		require.Eventually(t, func() bool { return stopCtx.Err() == context.Canceled }, time.Second, 20*time.Millisecond)
		require.Eventually(t, func() bool {
			v, _ := s.Eval(`document.querySelectorAll('[data-atlas-activity]').length`)
			return v == "0"
		}, time.Second, 20*time.Millisecond)
	}
	require.NoError(t, s.CloseActivity())
	count, err := s.Eval(`document.querySelectorAll('[data-atlas-activity]').length`)
	require.NoError(t, err)
	require.Equal(t, "0", count)
}

func TestActivityCannotInitializeAfterClose(t *testing.T) {
	s := &chromedpSession{activityEnabled: true}
	require.NoError(t, s.CloseActivity())
	s.BeginActivity(t.Context(), "closed-fixture", "click", "")()
	require.Nil(t, s.getActivityManager())
}
