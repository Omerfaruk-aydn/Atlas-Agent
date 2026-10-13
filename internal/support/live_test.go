
	// A new session hides the previous session's frame and timeline.
	c.remote = &Remote{SessionInfo: SessionInfo{SessionID: "ss-2"}}
	_, ok = c.LatestFrame()
	require.False(t, ok)
	require.Empty(t, c.Timeline(0))
}
