	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, http.StatusForbidden, res.StatusCode)

	c.mu.Lock()
	c.remote = &Remote{SessionInfo: SessionInfo{SessionID: "session-456", Generation: 2}}
	c.mu.Unlock()
	c.Record(Event{Kind: "operation", Detail: "new-session-private-operation"})
	r = get(v.URL() + "state")
	var state map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&state))
	r.Body.Close()
	require.Equal(t, false, state["frame"])
	require.Equal(t, "session_changed", state["error"])
	require.Nil(t, state["timeline"], "old panel may not expose new session events")
	r = get(v.URL() + "frame")
	require.Equal(t, http.StatusConflict, r.StatusCode)
	r.Body.Close()
}

func TestScreenViewerParentCancellationClosesListener(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	v, err := newScreenViewer(ctx, viewerController(t), "session-123")
	require.NoError(t, err)
	t.Cleanup(v.Close)
	cancel()
	select {
	case <-v.done:
	case <-time.After(3 * time.Second):
		t.Fatal("viewer listener leaked after controller cancellation")
	}
}

func TestTimelinePersistenceUsesOneTimestampAndRedactsLiveData(t *testing.T) {
	c := viewerController(t)
	c.Record(Event{Kind: "operation", CallID: "call-1", TaskID: "task-1", Detail: "token=do-not-keep", Fields: map[string]string{"message": "password=do-not-keep"}})
	live := c.Timeline(10)
	require.Len(t, live, 1)
	persisted, err := c.history.RecentEvents("session-123", 10)
	require.NoError(t, err)
	require.Len(t, persisted, 1)
	require.Equal(t, live[0].At, persisted[0].At)
	require.NotContains(t, live[0].Detail, "do-not-keep")
	require.False(t, strings.Contains(live[0].Fields["message"], "do-not-keep"))
	require.NoError(t, c.LoadTimeline("session-123"))
	require.Len(t, c.Timeline(10), 1, "reloading history must not duplicate live rows")
}

type countedPreviewDesktop struct {
	fakeDesktop
	captures atomic.Int32
}

func (d *countedPreviewDesktop) Screenshot() ([]byte, error) {
	d.captures.Add(1)
	return d.fakeDesktop.Screenshot()
}

func TestLiveViewerCapturesUntilStoppedWithoutTakingInputOwnership(t *testing.T) {
	desk := &countedPreviewDesktop{fakeDesktop: fakeDesktop{size: computer.Size{Width: 8, Height: 8}}}
	overlay := &fakeOverlay{}
	h := startHost(t, func(o *HostOptions) { o.Desktop = desk; o.Overlay = overlay })
	c := startController(t, nil)
	info := offerLAN(t, h, Presets["full"])
	joining := startJoin(c, info.Code+"@"+h.LANAddress())
	pending := waitPending(t, h)
	require.NoError(t, h.Decide(pending.ID, true, nil))
	require.NoError(t, (<-joining).err)
	overlay.mu.Lock()
	baselineStarts := overlay.started
	overlay.mu.Unlock()
	url, err := c.OpenScreenViewer()
	require.NoError(t, err)
	post := func(action string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+action, nil)
		require.NoError(t, err)
		req.Header.Set("Origin", "http://"+c.viewer.host)
		r, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		r.Body.Close()
		require.Equal(t, http.StatusNoContent, r.StatusCode)
	}
	post("start")
	require.Eventually(t, func() bool { return desk.captures.Load() >= 2 }, 5*time.Second, 20*time.Millisecond)
	frame, ok := c.LatestFrame()
	require.True(t, ok)
	require.NotEmpty(t, frame.PNG)
	overlay.mu.Lock()
	started := overlay.started
	inputRuns := overlay.open
	overlay.mu.Unlock()
	require.Equal(t, baselineStarts, started, "viewing must not start another control surface")
	require.Zero(t, inputRuns, "read-only human viewing must not acquire the agent's cursor")
	post("stop")
	count := desk.captures.Load()
	time.Sleep(1200 * time.Millisecond)
	require.Equal(t, count, desk.captures.Load(), "stop ended the capture goroutine")
	running, _ := c.viewer.watch.status()
	require.False(t, running)
}
