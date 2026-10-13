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
