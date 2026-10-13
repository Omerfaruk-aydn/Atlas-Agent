			_, _ = g.Auth.Decide(p.ID, true)
		}()
	}

	quit := make(chan struct{})
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /pair", func(w http.ResponseWriter, _ *http.Request) {
		u, _ := g.PairingURL()
		reply(w, map[string]string{"url": u})
	})
	mux.HandleFunc("POST /card", func(w http.ResponseWriter, r *http.Request) {
		req, _, err := vault.Ephemeral.Request(vault.CredentialSpec{SessionID: "s1", Service: "Example Okul", Origin: "https://login.example.test", Account: "ogrenci@example.test", Target: vault.CredentialTarget{TabID: "t", Document: "d", Selector: "#pw"}})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		reply(w, map[string]string{"id": req.ID})
	})
	mux.HandleFunc("GET /card/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, ok := vault.Ephemeral.Get(r.PathValue("id"))
		reply(w, map[string]any{"found": ok, "state": v.State, "provided_by": v.ProvidedBy, "persist": v.Persist})
	})
	// check-secret compares the stored value with the test's expectation
	// inside the gateway, then consumes it like a fill would.
	mux.HandleFunc("POST /card/{id}/check", func(w http.ResponseWriter, r *http.Request) {
		match := false
		_, err := vault.Ephemeral.Use(r.PathValue("id"), "s1", "https://login.example.test", "", func(s string) (bool, error) {
			match = s == r.URL.Query().Get("expect")
			return true, nil
		})
		reply(w, map[string]any{"match": match, "error": errString(err)})
	})
	mux.HandleFunc("POST /pause", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.interPaused, b.interCanceled, b.interReason = true, r.URL.Query().Get("canceled") == "1", r.URL.Query().Get("reason")
		b.revision += "p"
		b.mu.Unlock()
		reply(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		g.screen.mu.Lock()
		running, viewers := g.screen.running, len(g.screen.viewers)
		g.screen.mu.Unlock()
		reply(w, map[string]any{"controls": b.controls, "prompts": b.prompts, "runs": b.runs.Load(), "paused": b.interPaused, "screen_running": running, "screen_viewers": viewers, "captures": synthetic.calls.Load()})
	})
	mux.HandleFunc("POST /previews", func(w http.ResponseWriter, r *http.Request) {
		on := r.URL.Query().Get("on") == "1"
		for _, d := range g.Auth.Devices() {
			_ = g.Auth.SetPreviews(d.ID, on)
		}
		reply(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /screen", func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		delay, _ := strconv.Atoi(r.URL.Query().Get("delay_ms"))
		synthetic.set(func(s *syntheticScreen) {
			s.err, s.black, s.delay = nil, false, time.Duration(delay)*time.Millisecond
			switch mode {
			case "black":
				s.black = true
			case "error":
				s.err = errors.New("capture failed in test")
			case "portrait":
				s.width, s.height = 1080, 1920
			case "landscape":
				s.width, s.height = 1920, 1080
			}
		})
		reply(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, _ *http.Request) {
		n, err := g.Auth.RevokeAll()
		reply(w, map[string]any{"revoked": n, "error": errString(err)})
	})
	mux.HandleFunc("POST /quit", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, map[string]bool{"ok": true})
		close(quit)
	})
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctl := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = ctl.Serve(ln) }()
	defer ctl.Close()
	gateway := g.URLs()[0]
	for _, u := range g.URLs() {
		if strings.Contains(u, "127.0.0.1") {
			gateway = u
		}
	}
	data, _ := json.Marshal(map[string]string{"gateway": gateway, "control": "http://" + ln.Addr().String()})
	require.NoError(t, os.WriteFile(info, data, 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Minute)
	defer cancel()
	select {
	case <-quit:
	case <-ctx.Done():
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
