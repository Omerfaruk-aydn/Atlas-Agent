package activity

import (
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Set ATLAS_MASCOT_PREVIEW to record a scripted control run on the laptop:
// a greeting, typing, confirmed clicks, scrolling, navigation, thinking, a
// long question for the user, two failures in a row, the recovery and
// completion.
func TestMascotRunRecording(t *testing.T) {
	dir := os.Getenv("ATLAS_MASCOT_PREVIEW")
	if dir == "" {
		t.Skip("Set ATLAS_MASCOT_PREVIEW to record the laptop run")
	}
	banner := color.RGBA{23, 25, 32, 255}
	const scale = 4.0
	w, h := int(MascotWidth*scale), int(MascotHeight*scale)
	m := NewMascot()
	m.anim.perf.seed = 7
	start := time.Now()
	type step struct {
		at    time.Duration
		event Event
	}
	script := []step{
		{0, Event{Action: "type"}},
		{3500 * time.Millisecond, Event{Phase: PhaseThinking}},
		{5 * time.Second, Event{Action: "click", Point: true}},
		{8 * time.Second, Event{Action: "scroll"}},
		{10 * time.Second, Event{Action: "navigate"}},
		{11500 * time.Millisecond, Event{Phase: PhaseThinking}},
		{14 * time.Second, Event{Action: "observe"}},
		{16 * time.Second, Event{Phase: PhaseWaiting}},
		{25 * time.Second, Event{Action: "type"}},
		{27 * time.Second, Event{Phase: PhaseFailed}},
		{28500 * time.Millisecond, Event{Action: "click"}},
		{29500 * time.Millisecond, Event{Phase: PhaseFailed}},
		{31500 * time.Millisecond, Event{Action: "type"}},
		{35 * time.Second, Event{Phase: PhaseDone}},
	}
	clicks := []time.Duration{5600 * time.Millisecond, 6500 * time.Millisecond, 7400 * time.Millisecond}
	anim := &gif.GIF{}
	// Key moments, also written as one still sheet for review.
	moments := []time.Duration{300, 2200, 5700, 9000, 10400, 15000, 17500, 24000, 27600, 30200, 33200, 35400}
	sheet := image.NewRGBA(image.Rect(0, 0, w*6, h*2))
	shot := 0
	frame := time.Second / 30
	current := 0
	var e Event
	for d := time.Duration(0); d < 37*time.Second; d += frame {
		for current < len(script) && script[current].at <= d {
			e = script[current].event
			e.Visible, e.PhaseAt = true, start.Add(script[current].at)
			current++
		}
		for _, c := range clicks {
			if d >= c && d < c+frame {
				e.PointerKind, e.PointerAt = "click", start.Add(c)
			}
		}
		m.Advance(e, start.Add(d-frame/2), Look{X: -.3, Y: -.4})
		m.Advance(e, start.Add(d), Look{X: -.3, Y: -.4})
		pix := make([]byte, w*h*4)
		require.NoError(t, m.Render(pix, w, h, scale))
		tile := image.NewRGBA(image.Rect(0, 0, w, h))
		fill(tile, tile.Bounds(), banner)
		compositeMascot(tile, 0, 0, pix, w, h)
		if shot < len(moments) && d >= moments[shot]*time.Millisecond {
			draw.Draw(sheet, tile.Rect.Add(image.Pt((shot%6)*w, (shot/6)*h)), tile, image.Point{}, draw.Src)
			shot++
		}
		frameImage := image.NewPaletted(tile.Rect, palette.Plan9)
		draw.FloydSteinberg.Draw(frameImage, tile.Rect, tile, image.Point{})
		anim.Image = append(anim.Image, frameImage)
		anim.Delay = append(anim.Delay, 3)
	}
	writePNG(t, filepath.Join(dir, "mascot-laptop-moments.png"), sheet)
	f, err := os.Create(filepath.Join(dir, "mascot-laptop-run.gif"))
	require.NoError(t, err)
	require.NoError(t, gif.EncodeAll(f, anim))
	require.NoError(t, f.Close())
}
