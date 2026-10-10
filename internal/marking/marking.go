// Package marking turns a region or element the user marks on screen into a
// task attachment. A mark carries everything needed to find the same target
// again and to tell when it is no longer the same: monitor, DPI scale,
// virtual-screen coordinates, the owning window and process, an optional
// accessibility element, and a perceptual hash of the marked pixels.
//
// Coordinates are physical pixels in the virtual screen, whose origin is the
// top-left of the primary monitor; monitors left of or above it have
// negative coordinates. Marked content is data for the agent to inspect,
// never instructions.
package marking

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math/bits"
	"time"
)

// Rect is a physical-pixel rectangle in virtual-screen coordinates.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Right and Bottom are exclusive edges.
func (r Rect) Right() int  { return r.X + r.W }
func (r Rect) Bottom() int { return r.Y + r.H }

// Empty reports a zero-area rectangle.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Intersect returns the overlap of two rectangles.
func (r Rect) Intersect(o Rect) Rect {
	x1, y1 := max(r.X, o.X), max(r.Y, o.Y)
	x2, y2 := min(r.Right(), o.Right()), min(r.Bottom(), o.Bottom())
	if x2 <= x1 || y2 <= y1 {
		return Rect{}
	}
	return Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

// Contains reports whether the point is inside.
func (r Rect) Contains(x, y int) bool { return x >= r.X && y >= r.Y && x < r.Right() && y < r.Bottom() }

// Center returns the midpoint.
func (r Rect) Center() (int, int) { return r.X + r.W/2, r.Y + r.H/2 }

// FromDrag normalizes two drag corners in any direction into a rectangle.
func FromDrag(x1, y1, x2, y2 int) Rect {
	return Rect{X: min(x1, x2), Y: min(y1, y2), W: absInt(x2 - x1), H: absInt(y2 - y1)}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Monitor is one display.
type Monitor struct {
	ID      string `json:"id"`
	Bounds  Rect   `json:"bounds"`
	DPI     int    `json:"dpi"`
	Primary bool   `json:"primary"`
}

// Scale is the monitor's DPI scale (1.0 at 96 DPI).
func (m Monitor) Scale() float64 {
	if m.DPI <= 0 {
		return 1
	}
	return float64(m.DPI) / 96
}

// MonitorFor returns the monitor holding the largest part of r.
func MonitorFor(r Rect, monitors []Monitor) (Monitor, bool) {
	best, area := Monitor{}, 0
	for _, m := range monitors {
		i := m.Bounds.Intersect(r)
		if a := i.W * i.H; a > area {
			best, area = m, a
		}
	}
	return best, area > 0
}

// VirtualBounds is the union of all monitors.
func VirtualBounds(monitors []Monitor) Rect {
	if len(monitors) == 0 {
		return Rect{}
	}
	x1, y1 := monitors[0].Bounds.X, monitors[0].Bounds.Y
	x2, y2 := monitors[0].Bounds.Right(), monitors[0].Bounds.Bottom()
	for _, m := range monitors[1:] {
		x1, y1 = min(x1, m.Bounds.X), min(y1, m.Bounds.Y)
		x2, y2 = max(x2, m.Bounds.Right()), max(y2, m.Bounds.Bottom())
	}
	return Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

// CropToScreen converts a pixel inside the mark's crop image to a virtual
// screen point. The crop is stored at native resolution, so the transform is
// a translation; scale applies when a downscaled copy was shown.
func CropToScreen(mark Rect, imageScale float64, cx, cy int) (int, int, error) {
	if imageScale <= 0 {
		imageScale = 1
	}
	sx := mark.X + int(float64(cx)*imageScale+0.5)
	sy := mark.Y + int(float64(cy)*imageScale+0.5)
	if !mark.Contains(sx, sy) {
		return 0, 0, fmt.Errorf("crop point (%d,%d) maps to (%d,%d), outside the marked region", cx, cy, sx, sy)
	}
	return sx, sy, nil
}

// ToLogical converts a virtual-screen physical point to the monitor's
// logical (DPI-unaware, 96 DPI) coordinates relative to its origin, which is
// what a DPI-unaware target application sees.
func ToLogical(m Monitor, x, y int) (float64, float64) {
	s := m.Scale()
	return float64(x-m.Bounds.X) / s, float64(y-m.Bounds.Y) / s
}

// Window identifies the top-level window under a mark.
type Window struct {
	Handle  uint64 `json:"handle"`
	Title   string `json:"title"`
	PID     int    `json:"pid"`
	Process string `json:"process,omitempty"`
	Rect    Rect   `json:"rect"`
}

// Element is an accessibility element under a mark.
type Element struct {
	Name         string `json:"name,omitempty"`
	Role         string `json:"role,omitempty"`
	AutomationID string `json:"automation_id,omitempty"`
	Bounds       Rect   `json:"bounds"`
	// Verified is true when the element's bounds contain the marked point.
	Verified bool `json:"verified"`
}

// Browser links a mark to a page when the marked window is a browser Atlas
// controls and the page could be confirmed.
type Browser struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Verified bool   `json:"verified"`
}

// Validity states.
const (
	Fresh      = "fresh"
	Moved      = "moved"
	Stale      = "stale"
	Unverified = "unverified"
)

// Validity is the result of the last check against the live screen.
type Validity struct {
	State     string   `json:"state"`
	Reasons   []string `json:"reasons,omitempty"`
	CheckedAt int64    `json:"checked_at"`
	// Current is the region now, after re-resolving a moved window.
	Current *Rect `json:"current,omitempty"`
}

// Mark is the persisted selection.
type Mark struct {
	ID        string    `json:"id"`
	CreatedAt int64     `json:"created_at"`
	Source    string    `json:"source"`
	Mode      string    `json:"mode"`
	Note      string    `json:"note,omitempty"`
	Rect      Rect      `json:"rect"`
	Monitor   Monitor   `json:"monitor"`
	Monitors  []Monitor `json:"monitors"`
	Window    *Window   `json:"window,omitempty"`
	// Offset is the mark relative to the window's top-left, used to find
	// the region again after the window moves.
	Offset     *Rect    `json:"window_offset,omitempty"`
	Element    *Element `json:"element,omitempty"`
	Browser    *Browser `json:"browser,omitempty"`
	CropFile   string   `json:"crop_file"`
	CropSHA256 string   `json:"crop_sha256"`
	CropHash   uint64   `json:"crop_ahash"`
	// OverlayExcluded is true when Atlas overlays were excluded from the
	// capture (display affinity) and the capture preceded the selection UI.
	OverlayExcluded bool     `json:"overlay_excluded"`
	Validity        Validity `json:"validity"`
}

// ErrCanceled is returned when the user presses Escape.
var ErrCanceled = errors.New("marking canceled")

// AverageHash computes a 64-bit perceptual hash of img: 8x8 grayscale
// means compared with their average. Two captures of the same content
// differ by a few bits; different content differs by many.
func AverageHash(img image.Image) uint64 {
	b := img.Bounds()
	if b.Empty() {
		return 0
	}
	var cells [64]float64
	for gy := range 8 {
		for gx := range 8 {
			x0 := b.Min.X + gx*b.Dx()/8
			x1 := b.Min.X + (gx+1)*b.Dx()/8
			y0 := b.Min.Y + gy*b.Dy()/8
			y1 := b.Min.Y + (gy+1)*b.Dy()/8
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			stepX, stepY := max(1, (x1-x0)/8), max(1, (y1-y0)/8)
			sum, n := 0.0, 0
			for y := y0; y < y1 && y < b.Max.Y; y += stepY {
				for x := x0; x < x1 && x < b.Max.X; x += stepX {
					g := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
					sum += float64(g.Y)
					n++
				}
			}
			if n > 0 {
				cells[gy*8+gx] = sum / float64(n)
			}
		}
	}
	mean := 0.0
	for _, v := range cells {
		mean += v
	}
	mean /= 64
	var h uint64
	for i, v := range cells {
		if v > mean {
			h |= 1 << uint(i)
		}
	}
	return h
}

// HashDistance is the number of differing bits.
func HashDistance(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// maxContentDistance tolerates cursor blinks and anti-aliasing.
const maxContentDistance = 10

// Live is what validation needs from the running desktop.
type Live interface {
	Monitors() ([]Monitor, error)
	// WindowInfo reports a window by handle; ok=false if it is gone.
	WindowInfo(handle uint64) (Window, bool)
	// CaptureHash captures r and returns its AverageHash.
	CaptureHash(r Rect) (uint64, error)
}

// Validate checks a mark against the live desktop. A mark is fresh when its
// window is the same process at the same place and the pixels still match;
// moved when the window moved but the content followed it; stale when the
// window closed, changed owner, the monitor layout changed so the region is
// off-screen, or the content changed.
func Validate(m Mark, live Live, now time.Time) Validity {
	v := Validity{State: Fresh, CheckedAt: now.UnixMilli()}
	region := m.Rect
	monitors, err := live.Monitors()
	if err != nil {
		return Validity{State: Unverified, Reasons: []string{"cannot read monitor layout: " + err.Error()}, CheckedAt: v.CheckedAt}
	}
	if m.Window != nil && m.Window.Handle != 0 {
		w, ok := live.WindowInfo(m.Window.Handle)
		switch {
		case !ok:
			return Validity{State: Stale, Reasons: []string{"the marked window was closed"}, CheckedAt: v.CheckedAt}
		case w.PID != m.Window.PID:
			return Validity{State: Stale, Reasons: []string{"the window handle now belongs to another process"}, CheckedAt: v.CheckedAt}
		case w.Rect != m.Window.Rect && m.Offset != nil:
			region = Rect{X: w.Rect.X + m.Offset.X, Y: w.Rect.Y + m.Offset.Y, W: m.Offset.W, H: m.Offset.H}
			v.State = Moved
			v.Reasons = append(v.Reasons, fmt.Sprintf("window moved from (%d,%d) to (%d,%d)", m.Window.Rect.X, m.Window.Rect.Y, w.Rect.X, w.Rect.Y))
			if w.Rect.W != m.Window.Rect.W || w.Rect.H != m.Window.Rect.H {
				return Validity{State: Stale, Reasons: append(v.Reasons, "window was resized; the layout inside it may differ"), CheckedAt: v.CheckedAt}
			}
		}
	}
	if VirtualBounds(monitors).Intersect(region) != region {
		return Validity{State: Stale, Reasons: append(v.Reasons, "the region is no longer fully on any monitor"), CheckedAt: v.CheckedAt}
	}
	if mon, ok := MonitorFor(region, monitors); !ok || mon.DPI != m.Monitor.DPI && region == m.Rect {
		v.Reasons = append(v.Reasons, "monitor DPI changed since marking")
		v.State = Stale
		return v
	}
	h, err := live.CaptureHash(region)
	if err != nil {
		return Validity{State: Unverified, Reasons: append(v.Reasons, "cannot capture region: "+err.Error()), CheckedAt: v.CheckedAt}
	}
	if d := HashDistance(h, m.CropHash); d > maxContentDistance {
		return Validity{State: Stale, Reasons: append(v.Reasons, fmt.Sprintf("content changed (%d/64 hash bits differ)", d)), CheckedAt: v.CheckedAt}
	}
	if v.State == Moved {
		v.Current = &region
	}
	return v
}

// Targetable reports whether input may be aimed with the mark.
func (v Validity) Targetable() bool { return v.State == Fresh || v.State == Moved }
