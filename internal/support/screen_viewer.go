		}
	}
}

func (v *ScreenViewer) serve(w http.ResponseWriter, r *http.Request) {
	base := "/" + v.key + "/"
	if r.Host != v.host || !strings.HasPrefix(r.URL.Path, base) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	v.mu.Lock()
	v.lastSeen = time.Now()
	v.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, base)
	if r.Method == http.MethodPost {
		if r.Header.Get("Origin") != "http://"+v.host {
			http.Error(w, "wrong origin", http.StatusForbidden)
			return
		}
		if r.Body != nil {
			r.Body.Close()
		}
		status := v.controller.Status()
		if status.Remote == nil || status.Remote.SessionID != v.session {
			http.Error(w, "session changed", http.StatusConflict)
			return
		}
		switch path {
		case "start":
			if status.State != StateConnected && status.State != StatePaused {
				http.Error(w, "session is not connected", http.StatusConflict)
				return
			}
			v.watch.start(v.ctx, v.controller, v.session)
		case "stop":
			v.watch.stop()
		default:
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	switch path {
	case "state":
		name, state, frame, ok, timeline := v.controller.viewerSnapshot(v.session)
		running, watchErr := v.watch.status()
		if state == StateDisconnected {
			watchErr = "session_changed"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"device": name, "state": state, "watching": running && state.Active(), "error": watchErr, "frame": ok, "cached": frame.Stale(time.Now()), "captured_at": frame.CapturedAt, "generation": frame.Generation, "timeline": timeline})
	case "frame":
		_, state, f, ok, _ := v.controller.viewerSnapshot(v.session)
		if state == StateDisconnected {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", f.MediaType)
		_, _ = w.Write(f.PNG)
	default:
		if path == "" {
			path = "index.html"
		}
		if path != "index.html" && path != "app.js" && path != "app.css" {
			http.NotFound(w, r)
			return
		}
		sub, _ := fs.Sub(viewerFiles, "viewer")
		copy := r.Clone(r.Context())
		copy.URL.Path = "/" + path
		http.FileServerFS(sub).ServeHTTP(w, copy)
	}
}
