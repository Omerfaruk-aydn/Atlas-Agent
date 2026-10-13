	_, out = p.do("POST", "/api/questions/q1", map[string]any{"session_id": "s1", "cancel": true}, map[string]string{"Idempotency-Key": "q-2"})
	require.Equal(t, "stale_request", code(out))

	// A permission is answered exactly once.
	b.pending["perm-1"] = true
	g.Ingest(pubsub.Event[permission.PermissionRequest]{Payload: permission.PermissionRequest{ID: "perm-1", SessionID: "s1", ToolCallID: "call-1", ToolName: "bash", Action: "execute"}})
	resp, _ = p.do("POST", "/api/permissions/perm-1", map[string]string{"decision": "allow", "session_id": "s1", "tool_call_id": "call-1"}, map[string]string{"Idempotency-Key": "p-1"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_, out = p.do("POST", "/api/permissions/perm-1", map[string]string{"decision": "deny", "session_id": "s1", "tool_call_id": "call-1"}, map[string]string{"Idempotency-Key": "p-2"})
	require.Equal(t, "stale_request", code(out))

	// An image upload passes the content check through the proxy.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "shot.png")
	_, _ = fw.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00"))
	require.NoError(t, mw.Close())
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, p.base+"/api/sessions/s1/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", p.base)
	req.Header.Set("X-CSRF-Token", p.csrf)
	req.Header.Set("Idempotency-Key", "img-1")
	up, err := p.client.Do(req)
	require.NoError(t, err)
	up.Body.Close()
	require.Equal(t, http.StatusOK, up.StatusCode)

	// The screen preview stays off until enabled for this device.
	_, out = p.do("GET", "/api/preview", nil, nil)
	require.Equal(t, "previews_off", code(out))
	require.NoError(t, g.Auth.SetPreviews(g.Auth.Devices()[0].ID, true))
	resp, _ = p.do("GET", "/api/preview", nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFunnelRejectsForgedOriginCSRFAndHost(t *testing.T) {
	t.Parallel()
	f := newFunnelGateway(t, nil)
	p := phoneThroughFunnel(t, f.g, f.ingress)
	require.Equal(t, "accepted", p.pair(true))

	_, out := p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "x"}, map[string]string{"Idempotency-Key": "o1", "Origin": "https://evil.example"})
	require.Equal(t, "bad_origin", code(out))
	// The local origin is not the public one.
	_, out = p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "x"}, map[string]string{"Idempotency-Key": "o2", "Origin": "https://127.0.0.1"})
	require.Equal(t, "bad_origin", code(out))
	_, out = p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "x"}, map[string]string{"Idempotency-Key": "o3", "X-CSRF-Token": "forged"})
	require.Equal(t, "bad_csrf", code(out))
	require.Zero(t, f.b.runs.Load())

	for _, host := range []string{"other-pc.tail1234.ts.net", "atlas-pc.tail1234.ts.net:8443", "evil.example", "127.0.0.1", "localhost"} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, p.base+"/api/me", nil)
		req.Host = host
		resp, err := p.client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusMisdirectedRequest, resp.StatusCode, host)
	}

	// The public host is not accepted on the local TLS listener either.
	local := phoneVia(t, f.g, funnelHost)
	resp, _ := local.do("GET", "/api/me", nil, nil)
	require.Equal(t, http.StatusMisdirectedRequest, resp.StatusCode)
}

func TestFunnelUnpairedRevokedAndExpiredPhones(t *testing.T) {
	t.Parallel()
	f := newFunnelGateway(t, nil)
	stranger := phoneThroughFunnel(t, f.g, f.ingress)
	_, out := stranger.do("GET", "/api/sessions", nil, nil)
	require.Equal(t, "not_paired", code(out))
	_, out = stranger.do("POST", "/api/pair", map[string]string{"nonce": "guessed", "name": "x"}, nil)
	require.Equal(t, "pairing_invalid", code(out))
	at, _ := f.g.FunnelVerified()
	require.True(t, at.IsZero())

	p := phoneThroughFunnel(t, f.g, f.ingress)
	require.Equal(t, "accepted", p.pair(true))
	require.NoError(t, f.g.Auth.Revoke(f.g.Auth.Devices()[0].ID))
	resp, _ := p.do("GET", "/api/me", nil, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	p2 := phoneThroughFunnel(t, f.g, f.ingress)
	require.Equal(t, "accepted", p2.pair(true))
	f.g.Auth.now = func() time.Time { return time.Now().Add(SessionAbsolute + time.Hour) }
	resp, _ = p2.do("GET", "/api/me", nil, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestFunnelEventStreamReconnectsWithoutDuplicates(t *testing.T) {
	t.Parallel()
	f := newFunnelGateway(t, nil)
	p := phoneThroughFunnel(t, f.g, f.ingress)
	require.Equal(t, "accepted", p.pair(true))

	read := func(since string, want int) []string {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/api/events?since="+since, nil)
		resp, err := p.client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, "text/event-stream", strings.Split(resp.Header.Get("Content-Type"), ";")[0])
		var ids []string
		sc := bufio.NewScanner(resp.Body)
		for len(ids) < want && sc.Scan() {
			if id, ok := strings.CutPrefix(sc.Text(), "id: "); ok {
				ids = append(ids, id)
			}
		}
		return ids
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		f.g.Ingest(pubsub.Event[session.Session]{Payload: session.Session{ID: "s1", Title: "one"}})
		f.g.Ingest(pubsub.Event[session.Session]{Payload: session.Session{ID: "s1", Title: "two"}})
	}()
	first := read("0", 2)
	require.Len(t, first, 2, "events stream through the proxy without buffering")

	f.g.Ingest(pubsub.Event[session.Session]{Payload: session.Session{ID: "s1", Title: "three"}})
	again := read(first[0], 2)
	require.Equal(t, first[1], again[0], "a reconnect resumes after the last seen id")
	require.NotContains(t, again, first[0], "an event already delivered is not repeated")
}

func TestFunnelCloseRemovesEntryFirstAndOnlyOnce(t *testing.T) {
	t.Parallel()
	b := &fakeBackend{pending: map[string]bool{}, questions: map[string]bool{}}
	g, err := Start(t.Context(), Options{StateDir: t.TempDir(), Listen: "127.0.0.1:0", Backend: b})
	require.NoError(t, err)
	var calls atomic.Int32
	var listening atomic.Bool
	port, err := g.ServeFunnel(t.Context(), funnelHost, func() error {
		calls.Add(1)
		c, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp", "127.0.0.1:"+strconv.Itoa(g.FunnelLocalPort()))
		if err == nil {
			listening.Store(true)
			c.Close()
		}
		return errors.New("residual")
	})
	require.NoError(t, err)
	require.NotZero(t, port)
	_, err = g.ServeFunnel(t.Context(), funnelHost, nil)
	require.Error(t, err, "one Funnel per gateway")
	require.ErrorContains(t, g.Close(), "residual", "a failed removal is reported")
	require.True(t, listening.Load(), "the entry is removed while its target still listens")
	_ = g.Close()
	require.EqualValues(t, 1, calls.Load())
	_, err = (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	require.Error(t, err, "the loopback target is closed")
}

func TestFunnelRequestsWithoutTheFunnelHeaderProveNothing(t *testing.T) {
	t.Parallel()
	f := newFunnelGateway(t, nil)
	jar, _ := cookiejar.New(nil)
	direct := &phone{t: t, g: f.g, base: "http://127.0.0.1:" + strconv.Itoa(f.g.FunnelLocalPort()), client: &http.Client{Jar: jar}}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, direct.base+"/", nil)
	req.Host = funnelHost
	resp, err := direct.client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.True(t, f.g.FunnelSeen().IsZero())
}

func TestServeFunnelAcceptsOnlyTSNetNames(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"atlas-pc.tail1234.ts.net", "Atlas-PC.tail1234.ts.net", "atlas-pc.tail1234.ts.net:8443", "atlas-pc.tail1234.ts.net:10000"} {
		_, err := checkFunnelHost(ok)
		require.NoError(t, err, ok)
	}
	for _, bad := range []string{"", "*.ts.net", "ts.net", "x.ts.net", "evil.example", "atlas-pc.tail1234.ts.net:443", "atlas-pc.tail1234.ts.net:80", "atlas-pc.tail1234.ts.net.evil.com", "100.101.102.103", "atlas_pc.tail1234.ts.net", "https://atlas-pc.tail1234.ts.net"} {
		_, err := checkFunnelHost(bad)
		require.Error(t, err, bad)
	}
}
