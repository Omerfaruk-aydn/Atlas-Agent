//go:build windows

package activity

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// captureScreen copies a physical-pixel rectangle of the composed desktop.
func captureScreen(t *testing.T, area overlayRect) *image.RGBA {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if dpi := overlayUser.NewProc("SetThreadDpiAwarenessContext"); dpi.Find() == nil {
		previous, _, _ := dpi.Call(^uintptr(3))
		if previous != 0 {
			defer dpi.Call(previous)
		}
	}
	w, h := int(area.Right-area.Left), int(area.Bottom-area.Top)
	var surface nativeEdgeSurface
	require.True(t, surface.create(&edgePixels{width: w, height: h}))
	defer surface.close()
	screen, _, _ := overlayUser.NewProc("GetDC").Call(0)
	defer overlayUser.NewProc("ReleaseDC").Call(0, screen)
	overlayGDI.NewProc("BitBlt").Call(surface.dc, 0, 0, uintptr(w), uintptr(h), screen, uintptr(area.Left), uintptr(area.Top), 0x40CC0020)
	overlayGDI.NewProc("GdiFlush").Call()
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(surface.pixels); i += 4 {
		im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = surface.pixels[i+2], surface.pixels[i+1], surface.pixels[i], 255
	}
	return im
}

func saveFrame(t *testing.T, dir, name string, im image.Image) {
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, im))
	require.NoError(t, f.Close())
}

func windowVisible(hwnd uintptr) bool {
	v, _, _ := overlayUser.NewProc("IsWindowVisible").Call(hwnd)
	return v != 0
}

func exStyle(hwnd uintptr) uintptr {
	style, _, _ := overlayUser.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(19))
	return style
}

// TestControlIslandLive drives the real native island: it opens from the
// banner over a live blur, takes one answer, collapses, and tears down on
// cancellation mid-motion. Frames are written to ATLAS_ISLAND_FRAMES.
func TestControlIslandLive(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Set ATLAS_DESKTOP_FIXTURE=1 for native island verification")
	}
	frames := os.Getenv("ATLAS_ISLAND_FRAMES")
	if frames != "" {
		require.NoError(t, os.MkdirAll(frames, 0o755))
	}
	islandCaptureVisible = frames != ""
	defer func() { islandCaptureVisible = false }()
	language := os.Getenv("ATLAS_ISLAND_LANGUAGE")
	if language == "" {
		language = "tr"
	}

	r := newPlatformRenderer().(*windowsRenderer)
	m := New(r)
	defer m.Close()
	ctx, end := StartFlow(t.Context(), "island-live")
	defer end()
	ctx = WithLanguage(ctx, language)
	m.Begin(ctx, Event{Resource: "browser", Session: "island-live", Action: "navigate", Point: true, X: 500, Y: 400})()
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.frames > 5 && r.bannerWindow != 0 && windowVisible(r.bannerWindow)
	}, 5*time.Second, 10*time.Millisecond)
	r.mu.Lock()
	monitor := r.islandMonitor()
	r.mu.Unlock()
	area, scale, ok := monitorArea(monitor)
	require.True(t, ok)
	top := overlayRect{area.Left, area.Top, area.Right, area.Top + int32(math.Min(float64(area.Bottom-area.Top), 760*scale))}
	saveFrame(t, frames, "00-compact", captureScreen(t, top))

	var calls atomic.Int32
	var got PromptResponse
	var gotMu sync.Mutex
	prompt := Prompt{Kind: KindQuestion, ID: "live-question", Questions: []PromptQuestion{{
		ID: "live-q", Type: QuestionSingleChoice,
		Text:        "Hangi hesapla devam edeyim? Uzun bir soru metni, adanın satırları doğru kaydırdığını ve kritik anlamı kesmediğini göstermek için burada.",
		Description: "Seçimini yap; gönderene kadar hiçbir işlem yapılmayacak. **Not:** Çğışöü İÇĞŞÖÜ karakterleri doğru görünmeli.",
		Choices: []PromptChoice{
			{ID: "work", Label: "İş hesabı", Description: "ornek@sirket.test ile giriş"},
			{ID: "personal", Label: "Kişisel hesap"},
			{ID: "ask", Label: "Bana sonra sor", Description: "Bu adımı atla ve devam etme"},
		},
	}}}
	opened := time.Now()
	pending := AwaitPrompt(ctx, prompt, func(r PromptResponse) bool {
		calls.Add(1)
		gotMu.Lock()
		got = r
		gotMu.Unlock()
		return true
	})
	for i, at := range []time.Duration{40, 110, 180, 260, 420} {
		time.Sleep(time.Until(opened.Add(at * time.Millisecond)))
		saveFrame(t, frames, fmt.Sprintf("1%d-opening-%03dms", i, at), captureScreen(t, top))
	}
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		_, settled := r.island.motion.at(time.Now())
		return r.island.open && settled && r.blurAlpha(time.Now()) >= .999
	}, 3*time.Second, 10*time.Millisecond)
	r.mu.Lock()
	island, blur, cursor := r.bannerWindow, r.blurWindow, r.windows[0]
	require.True(t, r.blurLive, "DWM live blur must be active on this system")
	require.True(t, r.cursor.guard == nil || r.cursor.shown, "The user's own cursor is the only cursor while answering")
	frameCount, worst := r.islandFrameStats()
	layout := r.island.layout
	r.mu.Unlock()
	t.Logf("Opening: %d island frames, worst frame gap %s at scale %.2f", frameCount, worst, scale)
	require.True(t, windowVisible(blur), "The blur stays while the request waits")
	require.True(t, above(island, blur), "The island must sit above the blur")
	require.False(t, windowVisible(cursor), "No agent cursor while the user answers")
	require.Zero(t, exStyle(island)&0x20, "The island accepts input while a request is shown")
	require.Equal(t, StateAwaitQuestion, m.Snapshot().State)
	require.True(t, PromptBlocks(ctx, true), "Agent input waits for the answer")
	// A settled island must not reorder windows: every reorder makes DWM
	// recompose the blur, which the user sees as flicker.
	reorders := zOrderChanges.Load()
	time.Sleep(time.Second)
	require.Zero(t, zOrderChanges.Load()-reorders, "The settled overlay stack must stay still")
	saveFrame(t, frames, "20-waiting", captureScreen(t, top))

	// Pick the second option with the mouse, then send with Enter.
	var target islandHit
	for _, hit := range layout.hits {
		if hit.region == 1 && hit.control == 1 {
			target = hit
		}
	}
	require.NotZero(t, target.w)
	r.mu.Lock()
	g, _ := r.island.motion.at(time.Now())
	cw := float64(r.island.contentW) / scale
	r.mu.Unlock()
	x := int32((target.x + target.w/2 + (g.W/scale/2 - cw/2)) * scale)
	y := int32((layout.headerH + target.y + target.h/2) * scale)
	lparam := uintptr(uint32(x)) | uintptr(uint32(y))<<16
	post := func(msg uint32, w, l uintptr) { overlayUser.NewProc("PostMessageW").Call(island, uintptr(msg), w, l) }
	post(0x200, 0, lparam)
	post(0x201, 1, lparam)
	post(0x202, 0, lparam)
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.island.form != nil && r.island.form.isSelected("personal")
	}, 2*time.Second, 10*time.Millisecond)
	require.Zero(t, calls.Load(), "Selecting never sends")
	saveFrame(t, frames, "21-selected", captureScreen(t, top))
	post(0x100, 0x0d, 0)
	post(0x100, 0x0d, 0)
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int32(1), calls.Load(), "Repeated Enter sends once")
	gotMu.Lock()
	require.Equal(t, []string{"personal"}, got.Answers[0].Selected)
	gotMu.Unlock()
	saveFrame(t, frames, "22-sending", captureScreen(t, top))

	// The service resolves the request; the island returns to the strip.
	closed := time.Now()
	pending.Done(OutcomeAnswered)
	for i, at := range []time.Duration{60, 140, 220, 320} {
		time.Sleep(time.Until(closed.Add(at * time.Millisecond)))
		saveFrame(t, frames, fmt.Sprintf("3%d-closing-%03dms", i, at), captureScreen(t, top))
	}
	if !assertEventually(func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return !r.island.open && !r.island.motion.ready && !r.island.blurShown
	}) {
		r.mu.Lock()
		_, settled := r.island.motion.at(time.Now())
		defer r.mu.Unlock()
		t.Fatalf("Collapse incomplete: open=%t ready=%t settled=%t blurShown=%t alpha=%.3f to=%+v compact=%+v", r.island.open, r.island.motion.ready, settled, r.island.blurShown, r.blurAlpha(time.Now()), r.island.motion.to, r.island.compactRect)
	}
	require.False(t, windowVisible(blur), "Blur is removed after the answer")
	require.NotZero(t, exStyle(island)&0x20, "The strip is click-through again")
	require.Equal(t, StateResuming, m.Snapshot().State)
	require.False(t, PromptBlocks(ctx, true))
	saveFrame(t, frames, "40-resumed", captureScreen(t, top))

	// Cancel while a permission request is still opening.
	permission := AwaitPrompt(ctx, Prompt{Kind: KindPermission, ID: "live-permission", Permission: PromptPermission{
		Tool: "browser", Action: "click", Detail: "Click browser element: Satın al", Scope: `D:\Atlas`,
		Decisions: []PermissionDecision{DecisionAllowOnce, DecisionAllowSession, DecisionDeny},
	}}, func(PromptResponse) bool { calls.Add(1); return true })
	time.Sleep(120 * time.Millisecond)
	saveFrame(t, frames, "50-permission-opening", captureScreen(t, top))
	CancelSession("island-live")
	r.mu.Lock()
	require.False(t, r.island.open)
	require.False(t, r.island.blurShown, "Cancellation removes blur synchronously")
	require.Nil(t, r.cursor.guard, "Cancellation restores the system cursor")
	r.mu.Unlock()
	require.False(t, windowVisible(blur))
	require.False(t, windowVisible(island))
	require.Equal(t, int32(1), calls.Load(), "A cancelled request is never approved")
	permission.Done(OutcomeCancelled)
	saveFrame(t, frames, "51-cancelled", captureScreen(t, top))
	end()

	// A new task starts clean on the same renderer.
	next, endNext := StartFlow(context.Background(), "island-live-next")
	defer endNext()
	m.Begin(WithLanguage(next, language), Event{Resource: "desktop", Session: "island-live-next", Action: "click", Point: true, X: 600, Y: 420})()
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return windowVisible(r.bannerWindow) && !r.island.open
	}, 3*time.Second, 10*time.Millisecond)
	endNext()
}

func assertEventually(ok func() bool) bool {
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

// above reports whether upper precedes lower in the z-order.
func above(upper, lower uintptr) bool {
	for w := upper; w != 0; {
		w, _, _ = overlayUser.NewProc("GetWindow").Call(w, 2)
		if w == lower {
			return true
		}
	}
	return false
}
