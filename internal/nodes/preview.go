package nodes

import (
	"net/http"
	"time"
)

// handlePreview captures for a human viewer without replacing the agent's
// observation-bound input target. It has exactly the screen-view grant.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if err := r.Context().Err(); err != nil {
		return
	}
	// A human stream must not queue abandoned captures behind a slow
	// automation operation. Waiting for this shared lock is cancellable.
	for !s.desktopMu.TryLock() {
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-r.Context().Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	defer s.desktopMu.Unlock()
	if r.Context().Err() != nil {
		return
	}
	size, err := s.opts.Desktop.ScreenSize()
	if err != nil {
		writeError(w, http.StatusConflict, "capture_failed", err.Error(), false, "none")
		return
	}
	data, err := s.opts.Desktop.Screenshot()
	if err != nil {
		writeError(w, http.StatusConflict, "capture_failed", err.Error(), false, "none")
		return
	}
	if len(data) > 8<<20 {
		writeError(w, http.StatusConflict, "preview_too_large", "screen preview exceeds 8 MiB", false, "none")
		return
	}
	g := grantFrom(r)
	obs := DesktopObservation{NodeID: s.id.NodeID, SessionID: g.SessionID, Generation: g.Generation, Width: size.Width, Height: size.Height, PNG: data, CapturedAt: time.Now().UnixMilli(), Blank: isBlank(data), Monitor: "virtual", CoordinateSpace: "virtual_screen_physical_pixels", Scale: 1}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, obs)
}
