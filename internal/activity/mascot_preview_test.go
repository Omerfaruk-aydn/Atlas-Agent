package activity

import (
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The alternative constructions compared during design. They exist only to
// keep that comparison reproducible with the shipped renderer.
var (
	mascotGlobe = mascotScene{
		size: vec3{9.6, 9.6, 9.6}, half: 0, feet: 31,
		eyeY: 1.12, eyeSpread: .36, eyeW: 2.8, eyeH: 5.2,
		albedo: vec3{.80, .83, .87}, accessory: accessoryRing, tube: .8,
	}
	mascotPebble = mascotScene{
		size: vec3{10.2, 8.2, 8.8}, half: .12, feet: 31.4,
		eyeY: 1.2, eyeSpread: .36, eyeW: 2.8, eyeH: 5.2,
		albedo: vec3{.80, .83, .87}, accessory: accessoryAntenna, tube: .7,
	}
)

type mascotPreviewState struct {
	name  string
	event Event
	look  Look
	age   time.Duration
}

func mascotPreviewStates(start time.Time) []mascotPreviewState {
	at := func(d time.Duration) time.Time { return start.Add(-d) }
	return []mascotPreviewState{
		{"idle", Event{Visible: true, Action: "wait", PhaseAt: at(time.Hour)}, Look{}, 1200 * time.Millisecond},
		{"thinking", Event{Visible: true, Phase: PhaseThinking, PhaseAt: at(time.Hour)}, Look{}, 1200 * time.Millisecond},
		{"working", Event{Visible: true, Action: "click", PhaseAt: at(time.Hour)}, Look{X: -.7, Y: -.6}, 1200 * time.Millisecond},
		{"waiting", Event{Visible: true, Phase: PhaseWaiting, PhaseAt: at(time.Hour)}, Look{}, 1200 * time.Millisecond},
		{"blocked", Event{Visible: true, Phase: PhaseFailed}, Look{}, 520 * time.Millisecond},
		{"done", Event{Visible: true, Phase: PhaseDone}, Look{}, 420 * time.Millisecond},
	}
}

// Steady state for a mood without catching a blink.
func previewPose(t *testing.T, scene *mascotScene, s mascotPreviewState, browser bool) *Mascot {
	t.Helper()
	m := &Mascot{scene: scene}
	start := time.Now()
	e := s.event
	if browser {
		e.Resource = "browser"
	}
	if e.PhaseAt.IsZero() {
		e.PhaseAt = start
	}
	m.Advance(Event{Visible: true, Action: "wait", PhaseAt: start.Add(-time.Hour)}, start, Look{})
	m.anim.blink.next = start.Add(time.Hour)
	for d := time.Duration(0); d <= s.age; d += time.Second / 60 {
		m.Advance(e, start.Add(d), s.look)
	}
	return m
}

func compositeMascot(dst *image.RGBA, ox, oy int, pix []byte, w, h int) {
	for y := range h {
		for x := range w {
			k := (y*w + x) * 4
			a := float64(pix[k+3]) / 255
			c := dst.RGBAAt(ox+x, oy+y)
			dst.SetRGBA(ox+x, oy+y, color.RGBA{
				uint8(float64(pix[k]) + float64(c.R)*(1-a)),
				uint8(float64(pix[k+1]) + float64(c.G)*(1-a)),
				uint8(float64(pix[k+2]) + float64(c.B)*(1-a)), 255,
			})
		}
	}
}

func fill(im *image.RGBA, r image.Rectangle, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			im.SetRGBA(x, y, c)
		}
	}
}

func writePNG(t *testing.T, path string, im image.Image) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, im))
	require.NoError(t, f.Close())
}

// Set ATLAS_MASCOT_PREVIEW to a directory to write contact sheets.
func TestMascotPreviewSheets(t *testing.T) {
	dir := os.Getenv("ATLAS_MASCOT_PREVIEW")
	if dir == "" {
		t.Skip("Set ATLAS_MASCOT_PREVIEW to write mascot contact sheets")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	banner := color.RGBA{23, 25, 32, 255}
	scenes := []struct {
		name  string
		scene *mascotScene
	}{{"titan", &atlasTitan}, {"globe", &mascotGlobe}, {"pebble", &mascotPebble}}
	scales := []float64{1, 1.5, 2, 4}
	for _, sc := range scenes {
		for _, browser := range []bool{false, true} {
			if browser && sc.name != "titan" {
				continue
			}
			states := mascotPreviewStates(time.Now())
			// Rows are scales; each tile is magnified to the 4x tile size
			// with nearest sampling so pixel-level edges stay inspectable.
			tileW, tileH := MascotWidth*4, MascotHeight*4
			sheet := image.NewRGBA(image.Rect(0, 0, tileW*len(states), tileH*len(scales)))
			fill(sheet, sheet.Bounds(), banner)
			for col, s := range states {
				m := previewPose(t, sc.scene, s, browser)
				for row, scale := range scales {
					w, h := int(math.Round(MascotWidth*scale)), int(math.Round(MascotHeight*scale))
					pix := make([]byte, w*h*4)
					require.NoError(t, m.Render(pix, w, h, scale))
					tile := image.NewRGBA(image.Rect(0, 0, w, h))
					fill(tile, tile.Bounds(), banner)
					compositeMascot(tile, 0, 0, pix, w, h)
					for y := range tileH {
						for x := range tileW {
							sheet.SetRGBA(col*tileW+x, row*tileH+y, tile.RGBAAt(x*w/tileW, y*h/tileH))
						}
					}
				}
			}
			name := sc.name
			if browser {
				name += "-browser"
			}
			writePNG(t, filepath.Join(dir, "mascot-"+name+".png"), sheet)
		}
	}
}

// Set ATLAS_MASCOT_PREVIEW to write every gesture at its peak and a long
// improvised recording of the character slot.
func TestMascotGesturePreview(t *testing.T) {
	dir := os.Getenv("ATLAS_MASCOT_PREVIEW")
	if dir == "" {
		t.Skip("Set ATLAS_MASCOT_PREVIEW to write gesture previews")
	}
	banner := color.RGBA{23, 25, 32, 255}
	fillers, onsets := mascotLibrary()
	gestures := append(append([]mascotGesture{}, fillers...), onsets...)
	const scale, cols = 3.0, 8
	w, h := int(MascotWidth*scale), int(MascotHeight*scale)
	rows := (len(gestures) + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, w*cols, h*rows))
	fill(sheet, sheet.Bounds(), banner)
	moodEvent := map[mascotMood]Event{
		moodIdle: {Action: "wait"}, moodThinking: {Phase: PhaseThinking}, moodWorking: {Action: "click"},
		moodWaiting: {Phase: PhaseWaiting}, moodBlocked: {Phase: PhaseFailed}, moodDone: {Phase: PhaseDone},
	}
	for i, g := range gestures {
		mood := moodIdle
		for m := moodIdle; m <= moodDone; m++ {
			if g.moods&(1<<m) != 0 {
				mood = m
				break
			}
		}
		e := moodEvent[mood]
		e.Visible = true
		e.PhaseAt = time.Now().Add(-time.Hour)
		if mood == moodBlocked || mood == moodDone {
			e.PhaseAt = time.Now()
		}
		m := NewMascot()
		m.anim.reset()
		m.Advance(e, time.Now(), Look{})
		m.pose = m.anim.step(e, time.Now(), Look{})
		peak := .45
		if g.name == "spin" || g.name == "joy-spin" {
			peak = .35
		}
		g.play(peak, 1, 1).addTo(&m.pose, 1)
		m.pose.Open = [2]float64{max(.12, m.pose.Open[0]), max(.12, m.pose.Open[1])}
		pix := make([]byte, w*h*4)
		require.NoError(t, m.Render(pix, w, h, scale))
		compositeMascot(sheet, (i%cols)*w, (i/cols)*h, pix, w, h)
	}
	writePNG(t, filepath.Join(dir, "mascot-gestures.png"), sheet)

	// Forty seconds of unscripted life across moods, slot only.
	m := NewMascot()
	m.anim.perf.seed = 2026
	start := time.Now()
	script := []struct {
		at    time.Duration
		event Event
	}{
		{0, Event{Action: "wait"}},
		{14 * time.Second, Event{Action: "click", Point: true}},
		{19 * time.Second, Event{Phase: PhaseThinking}},
		{26 * time.Second, Event{Phase: PhaseWaiting}},
		{33 * time.Second, Event{Action: "type"}},
		{36 * time.Second, Event{Phase: PhaseFailed}},
		{38 * time.Second, Event{Phase: PhaseDone}},
	}
	anim := &gif.GIF{}
	frame := time.Second / 30
	current := 0
	var e Event
	for d := time.Duration(0); d < 40*time.Second; d += frame {
		for current < len(script) && script[current].at <= d {
			e = script[current].event
			e.Visible, e.PhaseAt = true, start.Add(script[current].at)
			current++
		}
		m.Advance(e, start.Add(d-frame/2), Look{X: -.4, Y: -.5})
		m.Advance(e, start.Add(d), Look{X: -.4, Y: -.5})
		pix := make([]byte, w*h*4)
		require.NoError(t, m.Render(pix, w, h, scale))
		tile := image.NewRGBA(image.Rect(0, 0, w, h))
		fill(tile, tile.Bounds(), banner)
		compositeMascot(tile, 0, 0, pix, w, h)
		frameImage := image.NewPaletted(tile.Rect, palette.Plan9)
		draw.FloydSteinberg.Draw(frameImage, tile.Rect, tile, image.Point{})
		anim.Image = append(anim.Image, frameImage)
		anim.Delay = append(anim.Delay, 3)
	}
	f, err := os.Create(filepath.Join(dir, "mascot-improv.gif"))
	require.NoError(t, err)
	require.NoError(t, gif.EncodeAll(f, anim))
	require.NoError(t, f.Close())
}
