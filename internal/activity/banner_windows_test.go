//go:build windows

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

func TestNativeBannerRendersReadableTextAtHighDPI(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 2, 4} {
		var b nativeBanner
		require.True(t, b.prepare(overlayRect{0, 0, 3840, 2160}, scale, Event{Language: "tr", CanStop: true}))
		require.Equal(t, int(math.Round(38*scale)), b.surface.model.height, "The slim strip must retain its height at fractional and high DPI")
		// Probe the settled pose: the laptop has grown in and no blink runs.
		mascot := NewMascot()
		start := time.Now()
		mascot.Advance(Event{Visible: true, Action: "wait"}, start, Look{})
		mascot.anim.blink.next = start.Add(time.Hour)
		mascot.anim.perf.next = start.Add(time.Hour)
		for d := time.Duration(0); d <= 2*time.Second; d += time.Second / 60 {
			mascot.Advance(Event{Visible: true, Action: "wait"}, start.Add(d), Look{})
		}
		b.paint(0, mascot, true)
		bright := 0
		for _, a := range b.glyphs {
			if a > 128 {
				bright++
			}
		}
		require.Greater(t, bright, 300, "The native status must contain actual antialiased text glyphs")
		pixels := b.surface.pixels
		at := func(x, y float64) byte {
			i := (int(y*scale)*b.surface.model.width + int(x*scale)) * 4
			return max(pixels[i], pixels[i+1], pixels[i+2])
		}
		require.Greater(t, at(24, 25.4), byte(150), "The character body must read as a lit volume")
		require.Less(t, at(20.9, 20.2), byte(90), "The character eyes must stay dark and distinct")
		require.False(t, b.mascotFailed)
		require.Zero(t, pixels[3], "Rounded corners must remain transparent")
		for i := 0; i < len(pixels); i += 4 {
			require.LessOrEqual(t, pixels[i], pixels[i+3])
			require.LessOrEqual(t, pixels[i+1], pixels[i+3])
			require.LessOrEqual(t, pixels[i+2], pixels[i+3])
		}
		if path := os.Getenv("ATLAS_BANNER_PREVIEW"); path != "" && scale == 2 {
			im := image.NewRGBA(image.Rect(0, 0, b.surface.model.width, b.surface.model.height))
			for i := 0; i < len(pixels); i += 4 {
				im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], pixels[i+3]
			}
			f, err := os.Create(path)
			require.NoError(t, err)
			require.NoError(t, png.Encode(f, im))
			require.NoError(t, f.Close())
		}
		b.close()
	}
}

func TestNativeBannerFallsBackToStaticIconWhenCharacterFails(t *testing.T) {
	var b nativeBanner
	require.True(t, b.prepare(overlayRect{0, 0, 1920, 1080}, 1.5, Event{Language: "en", CanStop: true}))
	defer b.close()
	mascot := NewMascot()
	mascot.draw = func(*mascotFrame, []byte, int, int) { panic("injected render failure") }
	mascot.Advance(Event{Visible: true}, time.Now(), Look{})
	b.paint(0, mascot, true)
	require.True(t, b.mascotFailed)
	pixels, scale := b.surface.pixels, 1.5
	at := func(x, y float64) byte {
		i := (int(y*scale)*b.surface.model.width + int(x*scale)) * 4
		return max(pixels[i], pixels[i+1], pixels[i+2])
	}
	require.Greater(t, at(25, 16), byte(120), "The static pointer icon replaces a failed character")
	require.Less(t, at(23, 21), byte(80), "The pointer interior must stay hollow")
	// Later frames keep the fallback without retrying a broken renderer.
	b.paint(time.Second, mascot, true)
	require.Zero(t, mascot.Frames)
	// The surviving layout stays usable: same height and stop area.
	require.Equal(t, int(math.Round(38*scale)), b.surface.model.height)
	require.NotZero(t, b.tail)
}

func TestDesktopEscapeIgnoresInjectedKeys(t *testing.T) {
	require.True(t, physicalEscape(0, 0x100, keyboardData{Key: 0x1b}))
	for _, key := range []keyboardData{{Key: 0x1b, Flags: 0x10}, {Key: 0x1b, Flags: 2}, {Key: 13}} {
		require.False(t, physicalEscape(0, 0x100, key))
	}
	require.False(t, physicalEscape(-1, 0x100, keyboardData{Key: 0x1b}))
	require.False(t, physicalEscape(0, 0x101, keyboardData{Key: 0x1b}))
}

func TestNativeBannerKeepsStopAreaOnNarrowMonitor(t *testing.T) {
	for _, language := range []string{"en", "tr", "de", "fr", "it", "ar"} {
		var b nativeBanner
		require.True(t, b.prepare(overlayRect{0, 0, 640, 480}, 2, Event{Language: language, CanStop: true}))
		require.LessOrEqual(t, b.surface.model.width, 608)
		// The caption may ellipsize, but it must not consume the stop area.
		bright := 0
		w := b.surface.model.width
		for y := range b.surface.model.height {
			for x := w - int(b.tail); x < w-32; x++ {
				if b.glyphs[y*w+x] > 128 {
					bright++
				}
			}
		}
		require.Greater(t, bright, 100, "Esc and the translated stop label must stay readable: %s", language)
		b.close()
	}
}

// bannerImage converts the native premultiplied BGRA surface for previews.
func bannerImage(b *nativeBanner) *image.RGBA {
	s := b.surface
	im := image.NewRGBA(image.Rect(0, 0, s.model.width, s.model.height))
	for i := 0; i < len(s.pixels); i += 4 {
		im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = s.pixels[i+2], s.pixels[i+1], s.pixels[i], s.pixels[i+3]
	}
	return im
}

// overDesktop composites a premultiplied banner over a desktop gradient.
func overDesktop(dst *image.RGBA, src *image.RGBA, ox, oy int) {
	for y := range src.Rect.Dy() {
		for x := range src.Rect.Dx() {
			s := src.RGBAAt(x, y)
			d := dst.RGBAAt(ox+x, oy+y)
			k := 255 - uint16(s.A)
			dst.SetRGBA(ox+x, oy+y, color.RGBA{byte(uint16(s.R) + uint16(d.R)*k/255), byte(uint16(s.G) + uint16(d.G)*k/255), byte(uint16(s.B) + uint16(d.B)*k/255), 255})
		}
	}
}

func desktop(w, h int, light bool) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			t := float64(x+y) / float64(w+h)
			c := color.RGBA{byte(13 + 30*t), byte(27 + 32*t), byte(42 + 40*t), 255}
			if light {
				c = color.RGBA{byte(246 - 40*t), byte(246 - 28*t), byte(241 - 14*t), 255}
			}
			im.SetRGBA(x, y, c)
		}
	}
	return im
}

// Set ATLAS_MASCOT_PREVIEW to a directory to write native banner previews
// and a short scripted animation recording.
func TestNativeBannerMascotPreview(t *testing.T) {
	dir := os.Getenv("ATLAS_MASCOT_PREVIEW")
	if dir == "" {
		t.Skip("Set ATLAS_MASCOT_PREVIEW to write native banner previews")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	start := time.Now()
	for _, light := range []bool{false, true} {
		sheet := desktop(1240, 470, light)
		y := 24
		for _, scale := range []float64{1, 1.5, 2} {
			x := 24
			for _, s := range mascotPreviewStates(start)[:3] {
				m := previewPose(t, &atlasTitan, s, false)
				var b nativeBanner
				require.True(t, b.prepare(overlayRect{0, 0, 3840, 2160}, scale, Event{Language: "tr", CanStop: true}))
				b.paint(0, m, true)
				im := bannerImage(&b)
				if x+im.Rect.Dx() > sheet.Rect.Dx() {
					b.close()
					break
				}
				overDesktop(sheet, im, x, y)
				x += im.Rect.Dx() + 16
				b.close()
			}
			y += int(math.Round(38*scale)) + 40
		}
		name := "banner-dark.png"
		if light {
			name = "banner-light.png"
		}
		f, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		require.NoError(t, png.Encode(f, sheet))
		require.NoError(t, f.Close())
	}
	// The three design directions in the same banner, idle, per scale.
	for _, light := range []bool{false, true} {
		sheet := desktop(1500, 290, light)
		directions := []*mascotScene{&mascotGlobe, &atlasTitan, &mascotPebble}
		for row, scene := range directions {
			x := 24
			for _, scale := range []float64{1, 1.5, 2} {
				m := previewPose(t, scene, mascotPreviewStates(start)[0], false)
				var b nativeBanner
				require.True(t, b.prepare(overlayRect{0, 0, 3840, 2160}, scale, Event{Language: "tr", CanStop: true}))
				b.paint(0, m, true)
				im := bannerImage(&b)
				overDesktop(sheet, im, x, 16+row*92+(76-im.Rect.Dy())/2)
				x += im.Rect.Dx() + 20
				b.close()
			}
		}
		name := "directions-dark.png"
		if light {
			name = "directions-light.png"
		}
		f, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		require.NoError(t, png.Encode(f, sheet))
		require.NoError(t, f.Close())
	}
	// A scripted run through every state, recorded at 30 fps and 2x.
	var b nativeBanner
	require.True(t, b.prepare(overlayRect{0, 0, 3840, 2160}, 2, Event{Language: "tr", CanStop: true}))
	defer b.close()
	m := NewMascot()
	script := []struct {
		at    time.Duration
		event Event
		look  Look
	}{
		{0, Event{Action: "wait"}, Look{}},
		{900 * time.Millisecond, Event{Action: "click", Point: true}, Look{X: -.6, Y: -.5}},
		{1300 * time.Millisecond, Event{Action: "click", Point: true, PointerKind: "click"}, Look{X: -.6, Y: -.5}},
		{1900 * time.Millisecond, Event{Phase: PhaseThinking}, Look{Y: -.25}},
		{3500 * time.Millisecond, Event{Action: "type"}, Look{X: .2, Y: -.7}},
		{4700 * time.Millisecond, Event{Phase: PhaseWaiting}, Look{Y: -.25}},
		{6000 * time.Millisecond, Event{Action: "navigate"}, Look{Y: -.3}},
		{6800 * time.Millisecond, Event{Phase: PhaseFailed}, Look{Y: -.25}},
		{8400 * time.Millisecond, Event{Action: "click", Point: true}, Look{X: .5, Y: -.4}},
		{9300 * time.Millisecond, Event{Phase: PhaseDone}, Look{}},
	}
	frame := time.Second / 30
	end := 10500 * time.Millisecond
	palette := append(color.Palette{}, palettePlan9()...)
	anim := &gif.GIF{}
	current := 0
	var e Event
	for d := time.Duration(0); d < end; d += frame {
		for current < len(script) && script[current].at <= d {
			e = script[current].event
			e.Visible, e.Resource = true, "desktop"
			e.PhaseAt = start.Add(script[current].at)
			if e.PointerKind == "click" {
				e.PointerAt = e.PhaseAt
			}
			current++
		}
		// 60 fps simulation, every second frame recorded.
		m.Advance(e, start.Add(d-frame/2), script[max(0, current-1)].look)
		m.Advance(e, start.Add(d), script[max(0, current-1)].look)
		b.paint(d, m, true)
		bg := desktop(b.surface.model.width+32, b.surface.model.height+32, false)
		overDesktop(bg, bannerImage(&b), 16, 16)
		frameImage := image.NewPaletted(bg.Rect, palette)
		draw.FloydSteinberg.Draw(frameImage, bg.Rect, bg, image.Point{})
		anim.Image = append(anim.Image, frameImage)
		anim.Delay = append(anim.Delay, 3)
	}
	f, err := os.Create(filepath.Join(dir, "banner-states.gif"))
	require.NoError(t, err)
	require.NoError(t, gif.EncodeAll(f, anim))
	require.NoError(t, f.Close())
}

func palettePlan9() color.Palette { return palette.Plan9 }

func BenchmarkNativeBannerFrame(b *testing.B) {
	for _, scale := range []float64{1, 2, 4} {
		b.Run("scale"+string(rune('0'+int(scale))), func(b *testing.B) {
			var banner nativeBanner
			require.True(b, banner.prepare(overlayRect{0, 0, 3840 * 2, 2160 * 2}, scale, Event{Language: "tr", CanStop: true}))
			defer banner.close()
			m := NewMascot()
			now := time.Now()
			b.ResetTimer()
			for i := range b.N {
				m.Advance(Event{Visible: true, Action: "click"}, now.Add(time.Duration(i)*time.Millisecond), Look{})
				banner.paint(time.Duration(i)*time.Millisecond, m, true)
			}
		})
	}
}
