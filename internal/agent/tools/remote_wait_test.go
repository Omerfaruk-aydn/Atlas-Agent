func TestRemoteWaitChangeIgnoresTheCaret(t *testing.T) {
	t.Parallel()
	g := func(v uint8) color.Gray { return color.Gray{Y: v} }
	s := &scriptedScreen{t: t, screens: []color.Gray{g(40), g(40), g(40), g(40), g(220)}}
	res, _, err := remoteWait(t.Context(), s, "change", "", "", time.Minute, time.Millisecond)
	require.NoError(t, err)
	require.True(t, res.Met)
	require.Equal(t, 5, res.Samples)
}

func TestRemoteWaitTextGoneAndTimeout(t *testing.T) {
	t.Parallel()
	s := &scriptedScreen{t: t, texts: []bool{true, true, false}}
	res, _, err := remoteWait(t.Context(), s, "text_gone", "Generating", "", time.Minute, time.Millisecond)
	require.NoError(t, err)
	require.True(t, res.Met)
	require.Equal(t, 3, res.Samples)

	never := &scriptedScreen{t: t, texts: []bool{true}}
	res, _, err = remoteWait(t.Context(), never, "text_gone", "Generating", "", 30*time.Millisecond, 5*time.Millisecond)
	require.NoError(t, err)
	require.False(t, res.Met, "timeout reports what was seen")

	_, _, err = remoteWait(t.Context(), never, "text", "", "", time.Second, time.Millisecond)
	require.ErrorContains(t, err, "needs name")
	_, _, err = remoteWait(t.Context(), never, "sleep", "", "", time.Second, time.Millisecond)
	require.Error(t, err)
}

func TestRemoteWaitIsCancellable(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	s := &scriptedScreen{t: t, texts: []bool{true}}
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, _, err := remoteWait(ctx, s, "text_gone", "Generating", "", time.Minute, 5*time.Millisecond)
	require.ErrorIs(t, err, context.Canceled)
}
