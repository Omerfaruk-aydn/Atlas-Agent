package browser

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
)

const (
	// The page receives frames from the same Go character engine as the
	// desktop banner; it only paints them.
	mascotFrameInterval = time.Second / 60
	mascotFrameScript   = `(()=>{const a=window[Symbol.for('atlas.agent.activity.v1')];return a&&a.mascot?a.mascot(%d,%d,%q):null})()`
)

// mascotStream owns one character per browser session. At most one frame
// worker runs at a time and each worker waits for its predecessor, so the
// animator is never shared between goroutines.
type mascotStream struct {
	mu      sync.Mutex
	event   activity.Event
	view    [3]float64 // devicePixelRatio, innerWidth, innerHeight.
	stop    chan struct{}
	done    chan struct{}
	mascot  *activity.Mascot
	painter func(w, h int, data string) ([]float64, error)
}

func (m *mascotStream) update(e activity.Event, visible bool, paint func(int, int, string) ([]float64, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.Visible = visible
	m.event = e
	switch {
	case visible && m.stop == nil:
		if m.mascot == nil {
			m.mascot = activity.NewMascot()
		}
		m.painter = paint
		previous := m.done
		m.stop, m.done = make(chan struct{}), make(chan struct{})
		go m.run(m.stop, m.done, previous)
	case !visible && m.stop != nil:
		// Never block the activity loop on page I/O; Close waits instead.
		close(m.stop)
		m.stop = nil
	}
}

// close stops frame delivery and waits for the worker to exit.
func (m *mascotStream) close() {
	m.mu.Lock()
	stop, done := m.stop, m.done
	m.stop = nil
	m.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	if done != nil {
		<-done
	}
}

func (m *mascotStream) running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stop != nil
}

func (m *mascotStream) run(stop chan struct{}, done chan<- struct{}, previous <-chan struct{}) {
	defer close(done)
	defer func() {
		// A worker that ends on its own lets the next event start a new one.
		m.mu.Lock()
		if m.stop == stop {
			m.stop = nil
		}
		m.mu.Unlock()
	}()
	if previous != nil {
		<-previous
	}
	m.mu.Lock()
	mascot, paint := m.mascot, m.painter
	m.mu.Unlock()
	defer mascot.Reset()
	ticker := time.NewTicker(mascotFrameInterval)
	defer ticker.Stop()
	var pix []byte
	var sent time.Time
	var sentID uint64
	var sentPhase activity.Phase
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		m.mu.Lock()
		e, view := m.event, m.view
		m.mu.Unlock()
		now := time.Now()
		if !e.Visible || (!e.FinishedUntil.IsZero() && now.After(e.FinishedUntil) && !e.Persistent) {
			return
		}
		mascot.Advance(e, now, pageLook(e, view))
		// Reduced motion only needs a new frame when the state changes.
		if e.ReducedMotion && e.ID == sentID && e.Phase == sentPhase && now.Sub(sent) < 250*time.Millisecond {
			continue
		}
		scale := max(1, min(4, view[0]))
		if view[0] == 0 {
			scale = 1
		}
		w, h := int(math.Round(activity.MascotWidth*scale)), int(math.Round(activity.MascotHeight*scale))
		if len(pix) != w*h*4 {
			pix = make([]byte, w*h*4)
		}
		if err := mascot.Render(pix, w, h, scale); err != nil {
			// The page keeps its static character; control is unaffected.
			slog.Warn("Browser banner character unavailable; keeping static icon", "error", err)
			<-stop
			return
		}
		unpremultiply(pix)
		result, err := paint(w, h, base64.StdEncoding.EncodeToString(pix))
		sent, sentID, sentPhase = now, e.ID, e.Phase
		if err == nil && len(result) == 3 {
			m.mu.Lock()
			m.view = [3]float64(result)
			m.mu.Unlock()
		}
	}
}

// ImageData expects straight alpha.
func unpremultiply(pix []byte) {
	for i := 0; i < len(pix); i += 4 {
		a := uint16(pix[i+3])
		if a == 0 || a == 255 {
			continue
		}
		for c := range 3 {
			pix[i+c] = byte(min(255, (uint16(pix[i+c])*255+a/2)/a))
		}
	}
}

func pageLook(e activity.Event, view [3]float64) activity.Look {
	if !e.Point || view[1] <= 0 || view[2] <= 0 {
		return activity.Look{Y: -.25}
	}
	x, y := float64(e.X), float64(e.Y)
	if e.PointerRevision != 0 {
		x, y = e.PointerX, e.PointerY
	}
	clamp := func(v float64) float64 { return max(-1, min(1, v)) }
	// The banner sits at the top center of the viewport.
	return activity.Look{X: clamp((x - view[1]/2) / (view[1] / 2)), Y: clamp((39 - y) / view[2] * 1.6)}
}

func (s *chromedpSession) paintMascot(w, h int, data string) ([]float64, error) {
	var result []float64
	err := s.activityEval(fmt.Sprintf(mascotFrameScript, w, h, data), &result)
	return result, err
}
