package activity

import (
	"image"
	"image/color"

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
