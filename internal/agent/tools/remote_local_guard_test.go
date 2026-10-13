	resp, err = gb.Run(ctx, navigate)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Zero(t, browser.runs)
	// The trusted human instruction, not tool arguments, selects local work.
	local := WithUserInstruction(ctx, "Bu bilgisayarda Chrome'u aç")
	resp, err = gb.Run(local, navigate)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Equal(t, 1, browser.runs)

	// No session: no check at all.
	session = ""
	other := &countingTool{name: BrowserToolName}
	resp, err = WithRemoteLocalGuard(other, target, NewRemoteGuardAcks()).Run(ctx, navigate)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Equal(t, 1, other.runs)

	// Other tools are untouched.
	view := &countingTool{name: ViewToolName}
	require.Same(t, fantasy.AgentTool(view), WithRemoteLocalGuard(view, target, acks))
}

// Reported session: 14 bash sleeps (11 min 36 s) waited for Stitch.
func TestSleepDuringSupportSessionPointsToRemoteWait(t *testing.T) {
	t.Parallel()
	session := "ss-d70f"
	target := func() (string, string) { return "Omerfaruk", session }
	bash := &countingTool{name: BashToolName}
	g := WithRemoteLocalGuard(bash, target, NewRemoteGuardAcks())
	for _, cmd := range []string{"sleep 58; echo ok", "sleep 45; echo done", "sleep 90", "Start-Sleep -Seconds 30"} {
		in, _ := json.Marshal(map[string]string{"command": cmd})
		resp, err := g.Run(t.Context(), fantasy.ToolCall{Name: BashToolName, Input: string(in)})
		require.NoError(t, err)
		require.True(t, resp.IsError, cmd)
		require.Contains(t, resp.Content, "remote_support action wait")
	}
	require.Zero(t, bash.runs)
	for _, cmd := range []string{"sleep 2", "go test ./... && sleep 30", "sleep 30 && ./deploy.sh"} {
		in, _ := json.Marshal(map[string]string{"command": cmd})
		resp, err := g.Run(t.Context(), fantasy.ToolCall{Name: BashToolName, Input: string(in)})
		require.NoError(t, err)
		require.False(t, resp.IsError, cmd)
	}
	session = ""
	in, _ := json.Marshal(map[string]string{"command": "sleep 58; echo ok"})
	resp, err := g.Run(t.Context(), fantasy.ToolCall{Name: BashToolName, Input: string(in)})
	require.NoError(t, err)
	require.False(t, resp.IsError, "no session: local sleeps are the user's business")
}
