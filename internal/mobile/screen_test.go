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
