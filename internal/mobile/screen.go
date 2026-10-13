		apiError(w, http.StatusForbidden, "bad_csrf", "missing or wrong CSRF token; reload the page")
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		apiError(w, http.StatusForbidden, "bad_origin", "live screen requests must come from this page")
		return
	}
	if g.rateLimited("screen:"+d.ID, 30) {
		apiError(w, http.StatusTooManyRequests, "rate_limited", "too many live screen connections; wait a moment")
		return
	}
	viewer, err := g.screen.join(d.ID)
	if err != nil {
		code := http.StatusConflict
		if errors.Is(err, errScreenBusy) {
			code = http.StatusTooManyRequests
		}
		apiError(w, code, "screen_unavailable", err.Error())
		return
	}
	defer g.screen.leave(viewer)
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()
	write := func(p *screenPacket) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(screenWriteTimeout))
		data := p.encode()
		if _, err := w.Write(data); err != nil {
			return false
		}
		if rc.Flush() != nil {
			return false
		}
		if p.kind == screenKindFrame {
			viewer.sent.Add(1)
		}
		viewer.bytes.Add(uint64(len(data)))
		return true
	}
	cookie, _ := r.Cookie(cookieName)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var lastSent, lastBytes uint64
	lastTick := time.Now()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-viewer.done:
			return
		case p := <-viewer.slot:
			if !write(p) {
				return
			}
		case p := <-viewer.status:
			var st screenStats
			if json.Unmarshal(p.payload, &st) == nil {
				now := time.Now()
				span := now.Sub(lastTick).Seconds()
				if span > 0 {
					sent, bytes := viewer.sent.Load(), viewer.bytes.Load()
					st.SentFPS = round1(float64(sent-lastSent) / span)
					st.BytesPerSec = float64(int(float64(bytes-lastBytes) / span))
					lastSent, lastBytes, lastTick = sent, bytes, now
				}
				st.Dropped = viewer.dropped.Load()
				p = &screenPacket{kind: p.kind, seq: p.seq, captured: p.captured, payload: mustJSON(st)}
			}
			if !write(p) {
				return
			}
		case <-tick.C:
			// Revocation, logout, expiry and turning viewing off all end
			// the stream within a second.
			dev, err := g.Auth.Authenticate(cookie.Value)
			if err != nil || dev.ID != d.ID || !dev.Previews {
				state := map[bool]string{true: "revoked", false: "previews_off"}[err != nil || dev.ID != d.ID]
				write(&screenPacket{kind: screenKindStatus, captured: time.Now(), payload: mustJSON(screenStats{State: state})})
				return
			}
		}
	}
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
