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
