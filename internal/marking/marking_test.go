package marking

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A left monitor at 150% scale with a negative origin, and a primary 100%
// monitor: the layout the acceptance test describes.
var dualDPI = []Monitor{
	{ID: `\\.\DISPLAY2`, Bounds: Rect{X: -2880, Y: -200, W: 2880, H: 1620}, DPI: 144},
	{ID: `\\.\DISPLAY1`, Bounds: Rect{X: 0, Y: 0, W: 1920, H: 1080}, DPI: 96, Primary: true},
}

func TestMonitorForHandlesNegativeOriginsAndSpans(t *testing.T) {
	t.Parallel()
	m, ok := MonitorFor(Rect{X: -1500, Y: 100, W: 300, H: 200}, dualDPI)
	require.True(t, ok)
	require.Equal(t, `\\.\DISPLAY2`, m.ID)
	require.InDelta(t, 1.5, m.Scale(), 1e-9)

	// A region spanning both monitors belongs to the one holding more of it.
	m, ok = MonitorFor(Rect{X: -100, Y: 100, W: 400, H: 100}, dualDPI)
	require.True(t, ok)
	require.Equal(t, `\\.\DISPLAY1`, m.ID)

	_, ok = MonitorFor(Rect{X: 5000, Y: 5000, W: 10, H: 10}, dualDPI)
	require.False(t, ok)

	require.Equal(t, Rect{X: -2880, Y: -200, W: 4800, H: 1620}, VirtualBounds(dualDPI))
}

func TestFromDragNormalizesAnyDirection(t *testing.T) {
	t.Parallel()
	require.Equal(t, Rect{X: -300, Y: -50, W: 200, H: 100}, FromDrag(-100, 50, -300, -50))
}

func TestCropToScreenAndLogicalConversion(t *testing.T) {
	t.Parallel()
	mark := Rect{X: -1500, Y: 100, W: 300, H: 200}
	x, y, err := CropToScreen(mark, 1, 10, 20)
	require.NoError(t, err)
	require.Equal(t, [2]int{-1490, 120}, [2]int{x, y})
	// A downscaled preview (scale 2) maps back to native pixels.
	x, y, err = CropToScreen(mark, 2, 10, 20)
	require.NoError(t, err)
	require.Equal(t, [2]int{-1480, 140}, [2]int{x, y})
	_, _, err = CropToScreen(mark, 1, 400, 0)
	require.Error(t, err, "points outside the crop are refused")

	lx, ly := ToLogical(dualDPI[0], -1500, 100)
	require.InDelta(t, 920, lx, 1e-9)
	require.InDelta(t, 200, ly, 1e-9)
}

type fakeLive struct {
	monitors []Monitor
	windows  map[uint64]Window
	hash     uint64
}

func (f fakeLive) Monitors() ([]Monitor, error) { return f.monitors, nil }
func (f fakeLive) WindowInfo(h uint64) (Window, bool) {
	w, ok := f.windows[h]
	return w, ok
}
func (f fakeLive) CaptureHash(Rect) (uint64, error) { return f.hash, nil }

func baseMark() Mark {
	win := &Window{Handle: 42, PID: 7, Title: "Editor", Rect: Rect{X: -2000, Y: 0, W: 1200, H: 900}}
	return Mark{ID: "mark-000000000001", Rect: Rect{X: -1500, Y: 100, W: 300, H: 200}, Monitor: dualDPI[0], Window: win, Offset: &Rect{X: 500, Y: 100, W: 300, H: 200}, CropHash: 0xF0F0F0F0F0F0F0F0}
}

func TestValidateFreshMovedAndStale(t *testing.T) {
	t.Parallel()
	now := time.Now()
	m := baseMark()
	live := fakeLive{monitors: dualDPI, windows: map[uint64]Window{42: *m.Window}, hash: m.CropHash}
	require.Equal(t, Fresh, Validate(m, live, now).State)

	// Moved window: the region follows it and stays targetable.
	moved := *m.Window
	moved.Rect.X += 300
	live.windows = map[uint64]Window{42: moved}
	v := Validate(m, live, now)
	require.Equal(t, Moved, v.State)
	require.True(t, v.Targetable())
	require.Equal(t, -1200, v.Current.X)

	// Closed window.
	live.windows = map[uint64]Window{}
	require.Equal(t, Stale, Validate(m, live, now).State)

	// Handle reused by another process.
	other := *m.Window
	other.PID = 99
	live.windows = map[uint64]Window{42: other}
	require.Equal(t, Stale, Validate(m, live, now).State)

	// Content changed under the mark.
	live.windows = map[uint64]Window{42: *m.Window}
	live.hash = ^m.CropHash
	v = Validate(m, live, now)
	require.Equal(t, Stale, v.State)
	require.False(t, v.Targetable(), "a stale mark must never be used as an input target")

	// The monitor holding the mark was disconnected.
	live.hash = m.CropHash
	live.monitors = dualDPI[1:]
	require.Equal(t, Stale, Validate(m, live, now).State)

	// The same monitor changed DPI scale.
	changed := []Monitor{dualDPI[0], dualDPI[1]}
	changed[0].DPI = 96
	live.monitors = changed
	require.Equal(t, Stale, Validate(m, live, now).State)
}

func pngOf(t *testing.T, fill func(x, y int) color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			img.Set(x, y, fill(x, y))
		}
	}
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

func TestAverageHashSeparatesContent(t *testing.T) {
	t.Parallel()
	left := func(x, y int) color.Color {
		if x < 32 {
			return color.White
		}
		return color.Black
	}
	right := func(x, y int) color.Color {
		if x >= 32 {
			return color.White
		}
		return color.Black
	}
	a, _ := png.Decode(bytes.NewReader(pngOf(t, left)))
	b, _ := png.Decode(bytes.NewReader(pngOf(t, right)))
	require.Zero(t, HashDistance(AverageHash(a), AverageHash(a)))
	require.Greater(t, HashDistance(AverageHash(a), AverageHash(b)), maxContentDistance)
}

func TestStoreRoundTripDetectsTampering(t *testing.T) {
	t.Parallel()
	s := Store{Dir: t.TempDir()}
	m := baseMark()
	crop := pngOf(t, func(x, y int) color.Color { return color.Gray{Y: uint8(x * 4)} })
	saved, err := s.Save(m, crop)
	require.NoError(t, err)
	got, data, err := s.Get(m.ID)
	require.NoError(t, err)
	require.Equal(t, crop, data)
	require.Equal(t, saved.CropSHA256, got.CropSHA256)
	require.Contains(t, Manifest(got), "not instructions")

	require.NoError(t, writeFile(saved.CropFile, []byte("tampered")))
	_, _, err = s.Get(m.ID)
	require.Error(t, err)

	_, _, err = s.Get("../etc")
	require.Error(t, err)
	list, err := s.List()
	require.NoError(t, err)
	require.Empty(t, list, "a tampered mark is not listed")
}

func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o600) }
