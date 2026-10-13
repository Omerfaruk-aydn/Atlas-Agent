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
