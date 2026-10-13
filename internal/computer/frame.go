var ErrFrameCaptureUnsupported = errors.New("computer-use: direct frame capture is not available on this platform")

// FrameScale returns the output size for a desktop of w×h limited to
// maxWidth, keeping the aspect ratio and even dimensions.
func FrameScale(w, h, maxWidth int) (int, int) {
	if maxWidth <= 0 || w <= maxWidth {
		return w, h
	}
	oh := h * maxWidth / w
	return maxWidth &^ 1, max(2, oh&^1)
}

// Uniform reports whether a frame is one flat color, sampled on a grid.
// A flat black frame usually means a locked, secure or disconnected
// desktop, which must not be presented as a live view.
func (f Frame) Uniform() (bool, [3]uint8) {
	img := f.Image
	if img == nil || img.Rect.Empty() {
		return true, [3]uint8{}
	}
	b := img.Rect
	first := img.RGBAAt(b.Min.X, b.Min.Y)
	for gy := range 17 {
		for gx := range 17 {
			x := b.Min.X + (b.Dx()-1)*gx/16
			y := b.Min.Y + (b.Dy()-1)*gy/16
			c := img.RGBAAt(x, y)
			if absDiff(c.R, first.R) > 3 || absDiff(c.G, first.G) > 3 || absDiff(c.B, first.B) > 3 {
				return false, [3]uint8{}
			}
		}
	}
	return true, [3]uint8{first.R, first.G, first.B}
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
