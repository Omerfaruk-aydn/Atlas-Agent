package activity

import (
	"math"
	"time"
)

const (
	pointerMoveDuration  = 180 * time.Millisecond
	pointerClickDuration = 260 * time.Millisecond
)

// Present flushes visual metadata without bypassing renderer cleanup locks.
func (m *Manager) Present() {
	m.renderMu.Lock()
	defer m.renderMu.Unlock()
	m.renderer.Render(m.Snapshot())
}

// Pointer records verified aiming or confirmed input for its owning operation.
func (m *Manager) Pointer(id uint64, kind string, x, y int) bool {
	return m.PointerPoint(id, kind, float64(x), float64(y))
}

// PointerPoint preserves fractional CSS coordinates for browser input.
func (m *Manager) PointerPoint(id uint64, kind string, x, y float64) bool {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || math.Abs(x) >= 1e7 || math.Abs(y) >= 1e7 {
		return false
	}
	switch kind {
	case "origin", "move", "aim", "press", "click", "drag", "drag_move", "release":
	default:
		return false
	}
	m.mu.Lock()
	if m.closed || !m.event.Visible || m.event.ID != id {
		m.mu.Unlock()
		return false
	}
	m.event.PointerRevision++
	m.event.PointerKind, m.event.PointerAt = kind, time.Now()
	m.event.X, m.event.Y, m.event.Point = int(math.Round(x)), int(math.Round(y)), true
	m.event.PointerX, m.event.PointerY = x, y
	m.mu.Unlock()
	m.signal()
	return true
}

type pointerMotion struct {
	ready                          bool
	x, y, fromX, fromY, aimX, aimY float64
	started                        time.Time
}

func (p *pointerMotion) position(x, y float64, snap bool, now time.Time) (float64, float64) {
	if !p.ready || snap {
		*p = pointerMotion{ready: true, x: x, y: y, fromX: x, fromY: y, aimX: x, aimY: y, started: now}
		return x, y
	}
	t := min(1, max(0, float64(now.Sub(p.started))/float64(pointerMoveDuration)))
	ease := 1 - (1-t)*(1-t)*(1-t)
	p.x, p.y = p.fromX+(p.aimX-p.fromX)*ease, p.fromY+(p.aimY-p.fromY)*ease
	if x != p.aimX || y != p.aimY {
		p.fromX, p.fromY, p.aimX, p.aimY, p.started = p.x, p.y, x, y, now
	}
	return p.x, p.y
}

func pointerCue(e Event, now time.Time) (press float64, held bool) {
	if e.PointerKind == "drag" || e.PointerKind == "drag_move" || e.PointerKind == "press" {
		return 1, true
	}
	if e.PointerKind == "click" {
		age := now.Sub(e.PointerAt)
		if age >= 0 && age < pointerClickDuration {
			if e.ReducedMotion {
				return 1, false
			}
			return 1 - float64(age)/float64(pointerClickDuration), false
		}
	}
	return 0, false
}
