	require.Nil(t, d.run(rcFunnelInstall))
	require.Equal(t, ActionRemoteFunnelTask{Task: FunnelTaskInstall}, press(d, "y"))

	*s = FunnelStatus{On: true, Task: FunnelTaskInstall, InstallStep: string(tsfunnel.StepVerify)}
	require.Contains(t, render(d, 120, 44), "Checking the SHA-256 and the Tailscale signature")
	require.NotContains(t, funnelActionIDs(d), rcFunnelInstall, "no second install while one runs")
}

func TestRemoteFunnelErrorsAreNamed(t *testing.T) {
	t.Parallel()
	s := &FunnelStatus{On: true, Checked: true, Ready: readyTailscale(), Err: &tsfunnel.PortConflictError{Occupants: []tsfunnel.Occupant{{Port: 443, What: "serve"}, {Port: 8443, What: "serve"}, {Port: 10000, What: "funnel"}}}}
	d := newFunnelDialog(t, "en", s)
	require.Contains(t, render(d, 120, 44), "All Funnel ports are in use")

	s.Err = tsfunnel.ErrResidual
	require.Contains(t, render(d, 120, 44), "still lists Atlas' Funnel entry")

	s.Err = errors.New("boom")
	require.Contains(t, render(d, 120, 44), "The Tailscale connection failed: boom")
}

func TestRemoteFunnelToRelaySwitchAsks(t *testing.T) {
	t.Parallel()
	p := config.RemoteProfile{ID: remotecontrol.NewProfileID(), Name: "vps", SSHHost: "203.0.113.10", SSHPort: 22, SSHUser: "root", SSHAuth: remotecontrol.AuthAgent, PublicHost: "atlas.example.com"}
	d, _ := newRemoteDialog(t, "en", p)
	s := FunnelStatus{On: true, Checked: true, Ready: readyTailscale(), URL: "https://atlas-pc.example.ts.net"}
	d.env.Funnel = func() FunnelStatus { return s }
	require.Nil(t, d.run(rcToggle))
	require.Equal(t, remoteConfirm, d.view)
	require.Contains(t, render(d, 120, 44), "Switch from Tailscale")
	require.IsType(t, ActionRemoteEnable{}, press(d, "y"))
}
