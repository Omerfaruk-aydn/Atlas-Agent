	target, err = s.PasswordTarget(t.Context(), "", "#pw")
	require.NoError(t, err)
	require.False(t, target.Found)
	id := s.ExecutionIdentity(t.Context())
	res, err = s.FillSecret(t.Context(), SecretTarget{Origin: origin, TabID: id.TabID, Document: id.Document, Selector: "#pw"}, "Synth-Should-Not-Arrive")
	require.Error(t, err)
	require.False(t, res.Dispatched)

	// Frames: a same-origin frame is fillable by its frame ID; a
	// cross-origin frame refuses the authorized origin.
	require.NoError(t, s.Navigate(origin+"/framed"))
	time.Sleep(300 * time.Millisecond)
	tree, err := s.command(s.currentContext(), "Page.getFrameTree", map[string]any{}, false)
	require.NoError(t, err)
	var sameID, crossID string
	root, _ := tree["frameTree"].(map[string]any)
	children, _ := root["childFrames"].([]any)
	for _, c := range children {
		f := c.(map[string]any)["frame"].(map[string]any)
		if strings.HasPrefix(f["url"].(string), origin) {
			sameID = f["id"].(string)
		} else {
			crossID = f["id"].(string)
		}
	}
	require.NotEmpty(t, sameID)
	require.NotEmpty(t, crossID)
	id = s.ExecutionIdentity(t.Context())
	res, err = s.FillSecret(t.Context(), SecretTarget{Origin: origin, TabID: id.TabID, Document: id.Document, FrameID: crossID, Selector: "#pw"}, "Synth-Should-Not-Arrive")
	require.Error(t, err)
	require.False(t, res.Dispatched)
	require.Equal(t, SecretOriginMismatch, res.Status)
	target, err = s.PasswordTarget(t.Context(), sameID, "")
	require.NoError(t, err)
	require.True(t, target.Found)
	res, err = s.FillSecret(t.Context(), SecretTarget{Origin: origin, TabID: id.TabID, Document: id.Document, FrameID: sameID, Selector: target.Selector, NodeToken: target.NodeToken}, "Synth-Frame-Pass")
	require.NoError(t, err)
	require.Equal(t, SecretFillDispatched, res.Status)
}

func waitLoginState(t *testing.T, s *chromedpSession, done func(LoginState) bool) LoginState {
	t.Helper()
	var last LoginState
	require.Eventually(t, func() bool {
		st, err := s.LoginState(t.Context(), "")
		if err != nil {
			return false
		}
		last = st
		return done(st)
	}, 5*time.Second, 50*time.Millisecond, "last state %+v", last)
	return last
}
