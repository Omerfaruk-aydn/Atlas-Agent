	st := s.nextStatus(t, "previews_off")
	require.Equal(t, "previews_off", st.State)
	_, err := s.r.ReadByte()
	require.ErrorIs(t, err, io.EOF, "the stream ends when viewing is turned off")

	require.NoError(t, g.Auth.SetPreviews(id, true))
	s2, _ := openPhoneStream(t, p)
	s2.nextFrame(t)
	start := time.Now()
	require.NoError(t, g.Auth.Revoke(id))
	for {
		_, err := s2.r.ReadByte()
		if err != nil {
			break
		}
	}
	require.Less(t, time.Since(start), 3*time.Second, "revocation ends the stream at once")
}

func TestScreenSharesOneCaptureLoopAndStopsAfterLastViewer(t *testing.T) {
	src := &syntheticScreen{width: 640, height: 360}
	g, b := newScreenGateway(t, src)
	p1 := newPhone(t, g)
	p1.pair(true)
	enablePreviews(t, g, true)
	p2 := newPhone(t, g)
	p2.pair(true)
	enablePreviews(t, g, true)

	s1, cancel1 := openPhoneStream(t, p1)
	s2, cancel2 := openPhoneStream(t, p2)
	before := src.calls.Load()
	var seen1, seen2 []uint64
	for range 12 {
		seen1 = append(seen1, s1.nextFrame(t).seq)
		seen2 = append(seen2, s2.nextFrame(t).seq)
	}
	captures := src.calls.Load() - before
	// Two viewers see the same frames from one loop; two loops would
	// capture about twice as often.
	require.LessOrEqual(t, captures, int64(20), "one capture loop serves every viewer")
	common := map[uint64]bool{}
	for _, s := range seen1 {
		common[s] = true
	}
	shared := 0
	for _, s := range seen2 {
		if common[s] {
			shared++
		}
	}
	require.Greater(t, shared, 6, "both viewers received the same encoded frames")

	cancel1()
	cancel2()
	require.Eventually(t, func() bool {
		g.screen.mu.Lock()
		defer g.screen.mu.Unlock()
		return !g.screen.running && len(g.screen.viewers) == 0
	}, 3*time.Second, 20*time.Millisecond, "capture stops after the last viewer leaves")
	stopped := src.calls.Load()
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, stopped, src.calls.Load())
	require.Zero(t, b.runs.Load(), "watching never starts or changes agent work")
	b.mu.Lock()
	require.Empty(t, b.controls)
	b.mu.Unlock()
}

func TestScreenSlowViewerGetsNewestFrameOnly(t *testing.T) {
	h := newScreenHub(&syntheticScreen{width: 320, height: 200}, nil)
	defer h.close()
	v, err := h.join("phone")
	require.NoError(t, err)
	// Never read: a slow phone. Its queue holds one frame at most.
	time.Sleep(400 * time.Millisecond)
	require.LessOrEqual(t, len(v.slot), 1)
	require.Positive(t, v.dropped.Load(), "older frames are dropped, not queued")
	newest := <-v.slot
	time.Sleep(100 * time.Millisecond)
	next := <-v.slot
	require.Greater(t, next.seq, newest.seq, "a late reader never gets an older frame after a newer one")
	h.leave(v)
}

func TestScreenCaptureFailureAndBlackDesktopAreNotFrames(t *testing.T) {
	src := &syntheticScreen{width: 640, height: 360, err: errors.New("BitBlt failed")}
	g, _ := newScreenGateway(t, src)
	p := newPhone(t, g)
	p.pair(true)
	enablePreviews(t, g, true)
	s, _ := openPhoneStream(t, p)
	st := s.nextStatus(t, "capture_error")
	require.Contains(t, st.Message, "BitBlt")

	src.set(func(s *syntheticScreen) { s.err = nil; s.black = true })
	st = s.nextStatus(t, "blank")
	require.Contains(t, st.Message, "black")
	black1 := s.nextFrame(t)
	black2 := s.nextFrame(t)
	require.Greater(t, black2.seq, black1.seq, "valid black frames continue streaming")
	src.set(func(s *syntheticScreen) { s.black = false })
	f := s.nextFrame(t)
	require.Positive(t, f.seq)
}

func TestScreenFollowsResolutionAndOrientationChanges(t *testing.T) {
	src := &syntheticScreen{width: 1280, height: 720}
	g, _ := newScreenGateway(t, src)
	p := newPhone(t, g)
	p.pair(true)
	enablePreviews(t, g, true)
	s, _ := openPhoneStream(t, p)
	f := s.nextFrame(t)
	require.Equal(t, [2]int{1280, 720}, [2]int{f.w, f.h})
	src.set(func(s *syntheticScreen) { s.width, s.height = 1080, 1920 })
	for range 50 {
		f = s.nextFrame(t)
		if f.w == 1080 {
			break
		}
	}
	require.Equal(t, [2]int{1080, 1920}, [2]int{f.w, f.h}, "portrait monitor")
}

func TestScreenSlowCaptureIsNeverOverlapped(t *testing.T) {
	src := &syntheticScreen{width: 320, height: 200, delay: 80 * time.Millisecond}
	var inFlight, maxInFlight atomic.Int32
	wrapped := ScreenSourceFunc(func(w int) (*image.RGBA, int, int, time.Time, error) {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		defer inFlight.Add(-1)
		return src.CaptureScreen(w)
	})
	h := newScreenHub(wrapped, nil)
	defer h.close()
	v, err := h.join("p")
	require.NoError(t, err)
	time.Sleep(600 * time.Millisecond)
	h.leave(v)
	require.EqualValues(t, 1, maxInFlight.Load())
	st, _ := h.snapshot()
	require.LessOrEqual(t, st.CaptureFPS, 13.0, "a slow source is reported at its real rate, not 24")
}

func TestScreenViewerLimit(t *testing.T) {
	h := newScreenHub(&syntheticScreen{width: 64, height: 64}, nil)
	defer h.close()
	var vs []*screenViewer
	for range screenMaxViewers {
		v, err := h.join("p")
		require.NoError(t, err)
		vs = append(vs, v)
	}
	_, err := h.join("p")
	require.ErrorIs(t, err, errScreenBusy)
	for _, v := range vs {
		h.leave(v)
	}
}
