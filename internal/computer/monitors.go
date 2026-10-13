package computer

// CoordinateSpacePhysical is the one coordinate contract shared by
// screenshots, window rectangles, monitor bounds, the cursor and input on
// Windows: physical pixels of the virtual screen, whose origin is the
// primary monitor's top-left corner. Monitors left of or above the primary
// have negative coordinates.
const CoordinateSpacePhysical = "virtual_screen_physical_pixels"

// MonitorObservation is one display in CoordinateSpacePhysical. DPI is the
// monitor's effective DPI and ScaleFactor is DPI/96 (1.25 at 125%); they
// describe how Windows scales applications, not a conversion that callers
// must apply: every coordinate here is already physical.
type MonitorObservation struct {
	Name            string  `json:"name"`
	Primary         bool    `json:"primary"`
	X               int     `json:"x"`
	Y               int     `json:"y"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	WorkX           int     `json:"work_x"`
	WorkY           int     `json:"work_y"`
	WorkWidth       int     `json:"work_width"`
	WorkHeight      int     `json:"work_height"`
	DPI             int     `json:"dpi"`
	ScaleFactor     float64 `json:"scale_factor"`
	CoordinateSpace string  `json:"coordinate_space"`
}

// MonitorAt returns the monitor containing the physical point, if any.
func MonitorAt(monitors []MonitorObservation, x, y int) (MonitorObservation, bool) {
	for _, m := range monitors {
		if x >= m.X && y >= m.Y && x < m.X+m.Width && y < m.Y+m.Height {
			return m, true
		}
	}
	return MonitorObservation{}, false
}

// VirtualBounds is the bounding rectangle of all monitors.
func VirtualBounds(monitors []MonitorObservation) (x, y, w, h int) {
	if len(monitors) == 0 {
		return 0, 0, 0, 0
	}
	minX, minY := monitors[0].X, monitors[0].Y
	maxX, maxY := minX+monitors[0].Width, minY+monitors[0].Height
	for _, m := range monitors[1:] {
		minX, minY = min(minX, m.X), min(minY, m.Y)
		maxX, maxY = max(maxX, m.X+m.Width), max(maxY, m.Y+m.Height)
	}
	return minX, minY, maxX - minX, maxY - minY
}
