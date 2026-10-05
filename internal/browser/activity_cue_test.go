package browser

import (
	"os"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/chromedp/cdproto/emulation"
	"github.com/stretchr/testify/require"
)

func TestActivityDelayedClickPaintsOnceAcrossOwnerRefresh(t *testing.T) {
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
	// CI desktops may disable animation; define the normal-motion test case.
	require.NoError(t, s.run(emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}})))
	require.NoError(t, s.Navigate("about:blank"))
	// Hold frame delivery so an expired input timestamp cannot pass accidentally.
	_, err = s.Eval(`window.cueFrames=[];window.requestAnimationFrame=cb=>(window.cueFrames.push(cb),window.cueFrames.length);window.cancelAnimationFrame=()=>{}`)
	require.NoError(t, err)
	e := activity.Event{ID: 1, Visible: true, Persistent: true, Point: true, PointerKind: "click", PointerRevision: 1, PointerAt: time.Now().Add(-2 * time.Second), PointerX: 120, PointerY: 100}
	renderer := browserActivityRenderer{s}
	renderer.Render(e)
	before, err := s.Eval(`(()=>{const a=window[Symbol.for('atlas.agent.activity.v1')];return {pending:a.cuePending,ring:Number(a.edges.parentNode.querySelector('.cursor-click-ring').getAttribute('opacity'))}})()`)
	require.NoError(t, err)
	require.JSONEq(t, `{"pending":true,"ring":0}`, before)
	painted, err := s.Eval(`(()=>{window.cueFrames.shift()(performance.now());const a=window[Symbol.for('atlas.agent.activity.v1')];return {pending:a.cuePending,visible:Number(a.edges.parentNode.querySelector('.cursor-click-ring').getAttribute('opacity'))>0}})()`)
	require.NoError(t, err)
	require.JSONEq(t, `{"pending":false,"visible":true}`, painted)
	// An owner refresh carrying the same confirmed input must not restart it.
	_, err = s.Eval(`(()=>{const a=window[Symbol.for('atlas.agent.activity.v1')];a.cueAt=performance.now()-500;a.drawPointer(performance.now(),true)})()`)
	require.NoError(t, err)
	e.ID++
	renderer.Render(e)
	refreshed, err := s.Eval(`(()=>{const a=window[Symbol.for('atlas.agent.activity.v1')];return {pending:a.cuePending,ring:Number(a.edges.parentNode.querySelector('.cursor-click-ring').getAttribute('opacity'))}})()`)
	require.NoError(t, err)
	require.JSONEq(t, `{"pending":false,"ring":0}`, refreshed)
	// A distinct confirmed input still gets feedback at the same coordinates.
	e.PointerRevision++
	e.PointerAt = e.PointerAt.Add(time.Millisecond)
	renderer.Render(e)
	pending, err := s.Eval(`window[Symbol.for('atlas.agent.activity.v1')].cuePending`)
	require.NoError(t, err)
	require.Equal(t, "true", pending)
	e.PointerRevision++
	e.PointerAt = e.PointerAt.Add(time.Millisecond)
	e.ReducedMotion = true
	renderer.Render(e)
	reduced, err := s.Eval(`(()=>{const a=window[Symbol.for('atlas.agent.activity.v1')];return {pending:a.cuePending,ring:Number(a.edges.parentNode.querySelector('.cursor-click-ring').getAttribute('opacity'))}})()`)
	require.NoError(t, err)
	require.JSONEq(t, `{"pending":false,"ring":0}`, reduced)
	renderer.Close()
}
