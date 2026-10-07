package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/stretchr/testify/require"
)

func TestUnpremultiplyRestoresStraightAlpha(t *testing.T) {
	t.Parallel()
	pix := []byte{64, 32, 0, 128, 10, 20, 30, 255, 0, 0, 0, 0}
	unpremultiply(pix)
	require.Equal(t, []byte{128, 64, 0, 128, 10, 20, 30, 255, 0, 0, 0, 0}, pix)
}

func TestPageLookAimsAtVerifiedTarget(t *testing.T) {
	t.Parallel()
	view := [3]float64{2, 1000, 800}
	require.Equal(t, activity.Look{Y: -.25}, pageLook(activity.Event{}, view))
	look := pageLook(activity.Event{Point: true, PointerRevision: 1, PointerX: 1000, PointerY: 800}, view)
	require.InDelta(t, 1, look.X, 1e-9)
	require.Less(t, look.Y, -.9)
}

func TestActivityMascotStreamsFramesFromGoEngine(t *testing.T) {
	exe := ""
	for _, path := range []string{"C:/Program Files/Google/Chrome/Application/chrome.exe", "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "/usr/bin/chromium", "/usr/bin/google-chrome"} {
		if _, err := os.Stat(path); err == nil {
			exe = path
			break
		}
	}
	if exe == "" {
		t.Skip("Chrome/Chromium not installed")
	}
	session, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), ActionTimeout: 30 * time.Second})
	require.NoError(t, err)
	defer session.Close()
	s := session.(*chromedpSession)
	require.NoError(t, s.run(emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}})))
	require.NoError(t, s.run(emulation.SetDeviceMetricsOverride(1280, 800, 2, false)))
	require.NoError(t, s.Navigate("about:blank"))
	renderer := browserActivityRenderer{s}
	state := func() map[string]any {
		raw, err := s.Eval(`(()=>{const a=window[Symbol.for('atlas.agent.activity.v1')],r=a.edges.parentNode,c=r.querySelector('.mascot-live'),b=r.querySelector('.control-banner'),cap=r.querySelector('.caption');
			let lit=0;if(c.width){const d=c.getContext('2d').getImageData(0,0,c.width,c.height).data;for(let i=3;i<d.length;i+=4)if(d[i]>200)lit++}
			return {live:!c.hidden,static:!r.querySelector('.mascot-static').hidden,width:c.width,height:c.height,lit,frames:window.atlasFrames||0,
			height_css:b.getBoundingClientRect().height,text:cap.getBoundingClientRect().left-b.getBoundingClientRect().left,
			cursor:r.querySelector('.cursor').style.display,edges:a.edges.style.display}})()`)
		require.NoError(t, err)
		var out map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &out))
		return out
	}
	e := activity.Event{ID: 1, Visible: true, Persistent: true, Resource: "browser", Action: "click", PhaseAt: time.Now()}
	renderer.Render(e)
	first := state()
	require.Equal(t, true, first["static"], "The static character covers the time before the first frame")
	_, err = s.Eval(`(()=>{const a=window[Symbol.for('atlas.agent.activity.v1')],m=a.mascot.bind(a);a.mascot=(...v)=>{window.atlasFrames=(window.atlasFrames||0)+1;return m(...v)}})()`)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return state()["live"] == true }, 3*time.Second, 20*time.Millisecond)
	before := state()
	time.Sleep(time.Second)
	live := state()
	require.Equal(t, false, live["static"])
	require.EqualValues(t, activity.MascotWidth*2, live["width"], "Frames match the page's device pixel ratio")
	require.EqualValues(t, activity.MascotHeight*2, live["height"])
	require.Greater(t, live["lit"].(float64), 400.0, "The canvas holds the rendered character")
	require.InDelta(t, 38, live["height_css"].(float64), .5, "The banner keeps its height")
	require.InDelta(t, 46, live["text"].(float64), 1.5, "The caption keeps its position")
	fps := live["frames"].(float64) - before["frames"].(float64)
	t.Logf("Browser banner character frames delivered in 1s: %.0f", fps)
	require.Greater(t, fps, 20.0, "Frames keep flowing while visible")

	if dir := os.Getenv("ATLAS_MASCOT_PREVIEW"); dir != "" {
		var shot []byte
		require.NoError(t, chromedp.Run(s.currentContext(), chromedp.CaptureScreenshot(&shot)))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "browser-banner.png"), shot, 0o644))
	}

	// Completion: control indicators go at once, the banner settles alone.
	e.ID, e.Visible, e.Persistent, e.Phase, e.PhaseAt = 2, false, false, activity.PhaseDone, time.Now()
	e.FinishedUntil = e.PhaseAt.Add(400 * time.Millisecond)
	renderer.Render(e)
	done := state()
	require.Equal(t, "none", done["cursor"])
	require.Equal(t, "none", done["edges"])
	require.True(t, s.activityMascot.running())
	require.Eventually(t, func() bool { return !s.activityMascot.running() }, 2*time.Second, 10*time.Millisecond, "The worker ends with the banner")

	// Hiding stops delivery; closing waits for the worker.
	e = activity.Event{ID: 3, Visible: true, Persistent: true, Resource: "browser", Phase: activity.PhaseThinking, PhaseAt: time.Now()}
	renderer.Render(e)
	require.True(t, s.activityMascot.running())
	renderer.Render(activity.Event{ID: 4})
	require.False(t, s.activityMascot.running())
	renderer.Render(e)
	renderer.Close()
	require.False(t, s.activityMascot.running())
	select {
	case <-s.activityMascot.done:
	default:
		t.Fatal("Close must wait for the frame worker")
	}
}
