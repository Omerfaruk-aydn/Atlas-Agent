	require.NotEqual(t, "", code(out))

	b.mu.Lock()
	b.interCanceled = true
	b.mu.Unlock()
	resp, out := p.do("POST", "/api/sessions/s1/control", map[string]string{"action": "interaction_resume", "revision": rev}, map[string]string{"Idempotency-Key": "c3"})
	require.Equal(t, 200, resp.StatusCode, out)
	require.Equal(t, true, out["canceled"])
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, b.runs.Load(), "a canceled task is not revived")
	b.mu.Lock()
	require.Equal(t, []string{"interaction_resume"}, b.controls)
	b.mu.Unlock()

	// The same request replayed (double tap) is answered from the store.
	resp, _ = p.do("POST", "/api/sessions/s1/control", map[string]string{"action": "interaction_resume", "revision": rev}, map[string]string{"Idempotency-Key": "c3"})
	require.Equal(t, "true", resp.Header.Get("Idempotent-Replay"))
	b.mu.Lock()
	require.Len(t, b.controls, 1)
	b.mu.Unlock()
}

func TestHomeScreenTransferCode(t *testing.T) {
	g, _ := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	resp, out := p.do("POST", "/api/transfer", map[string]any{}, map[string]string{"Idempotency-Key": "t1"})
	require.Equal(t, 200, resp.StatusCode, out)
	codeText := out["code"].(string)
	require.Len(t, strings.ReplaceAll(codeText, "-", ""), 8)

	app := newPhone(t, g)
	resp, out = app.do("POST", "/api/transfer/redeem", map[string]string{"code": strings.ToLower(codeText)}, nil)
	require.Equal(t, 200, resp.StatusCode, out)
	require.Contains(t, resp.Header.Get("Set-Cookie"), "HttpOnly")
	app.csrf = out["csrf"].(string)
	resp, me := app.do("GET", "/api/me", nil, nil)
	require.Equal(t, 200, resp.StatusCode)
	require.Contains(t, me["device"].(map[string]any)["name"], "(Home Screen)")

	again := newPhone(t, g)
	_, out = again.do("POST", "/api/transfer/redeem", map[string]string{"code": codeText}, nil)
	require.Equal(t, "transfer_invalid", code(out), "a code works once")
	_, out = again.do("POST", "/api/transfer/redeem", map[string]string{"code": codeText}, map[string]string{"Origin": "https://evil.example"})
	require.Equal(t, "bad_origin", code(out))
	unpaired := newPhone(t, g)
	resp, _ = unpaired.do("POST", "/api/transfer", map[string]any{}, map[string]string{"Idempotency-Key": "t2"})
	require.Equal(t, 401, resp.StatusCode, "only a paired browser can issue a code")

	// Expired codes fail.
	resp, out = p.do("POST", "/api/transfer", map[string]any{}, map[string]string{"Idempotency-Key": "t3"})
	require.Equal(t, 200, resp.StatusCode)
	g.Auth.now = func() time.Time { return time.Now().Add(TransferTTL + time.Second) }
	_, out2 := newPhone(t, g).do("POST", "/api/transfer/redeem", map[string]string{"code": out["code"].(string)}, nil)
	g.Auth.now = time.Now
	require.Equal(t, "transfer_invalid", code(out2))
}

func mustString(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// An idle event stream sends a first line at once: WebKit keeps an
// EventSource in the connecting state until body bytes arrive, which left
// the phone's buttons disabled for up to one keepalive interval.
func TestEventStreamOpensWithoutWaitingForEvents(t *testing.T) {
	t.Parallel()
	g, _ := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/api/events", nil)
	resp, err := p.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	require.NoError(t, err, "the first bytes must not wait for an event or keepalive")
	require.True(t, strings.HasPrefix(line, ":"), line)
}
