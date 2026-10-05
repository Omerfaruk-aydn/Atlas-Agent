package browser

import (
	"encoding/json"
	"math"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
)

func mouseCoordinate(value any) (float64, bool) {
	var f float64
	switch v := value.(type) {
	case float64:
		f = v
	case int:
		f = float64(v)
	case json.Number:
		var err error
		f, err = v.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return f, !math.IsNaN(f) && !math.IsInf(f, 0) && math.Abs(f) < 1e7
}

func (s *chromedpSession) presentMouseAim(m *activity.Manager, id uint64, params map[string]any) {
	x, okX := mouseCoordinate(params["x"])
	y, okY := mouseCoordinate(params["y"])
	if okX && okY {
		presentPointer(m, id, "aim", x, y)
	}
}

func (s *chromedpSession) presentMouseEvent(m *activity.Manager, id uint64, params map[string]any) {
	x, okX := mouseCoordinate(params["x"])
	y, okY := mouseCoordinate(params["y"])
	if !okX || !okY {
		return
	}
	kind := ""
	s.mu.Lock()
	s.pointerX, s.pointerY = x, y
	switch params["type"] {
	case "mousePressed":
		s.pointerHeld, s.pointerDragged = true, false
		kind = "press"
	case "mouseMoved":
		kind = "move"
		buttons, hasButtons := mouseCoordinate(params["buttons"])
		if s.pointerHeld || (hasButtons && buttons != 0) {
			s.pointerHeld, s.pointerDragged = true, true
			kind = "drag_move"
		}
	case "mouseReleased":
		kind = "click"
		if s.pointerDragged {
			kind = "release"
		}
		s.pointerHeld, s.pointerDragged = false, false
	}
	s.mu.Unlock()
	presentPointer(m, id, kind, x, y)
}
