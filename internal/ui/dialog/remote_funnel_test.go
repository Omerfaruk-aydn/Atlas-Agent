		{tsfunnel.ProblemServiceStopped, rcFunnelApp, "The Tailscale service is not running"},
		{tsfunnel.ProblemNeedsLogin, rcFunnelLogin, "Tailscale sign-in needed"},
		{tsfunnel.ProblemDisconnected, rcFunnelLogin, "Tailscale is disconnected"},
		{tsfunnel.ProblemNeedsMagicDNS, rcFunnelDNS, "MagicDNS and HTTPS are needed"},
		{tsfunnel.ProblemNoFunnelPort, rcFunnelCheck, "Funnel is not allowed on any port"},
		{tsfunnel.ProblemMachineAuth, rcFunnelCheck, "Waiting for device approval"},
	} {
		t.Run(string(tc.problem), func(t *testing.T) {
			t.Parallel()
			s := &FunnelStatus{On: true, Checked: true, Ready: tsfunnel.Readiness{Problem: tc.problem}}
			d := newFunnelDialog(t, "en", s)
			ids := funnelActionIDs(d)
			require.Contains(t, ids, tc.action)
			require.Contains(t, ids, rcFunnelCheck)
			require.Contains(t, ids, rcFunnelStop)
			require.NotContains(t, ids, rcFunnelPair, "no QR before the Funnel is set up")
			require.Contains(t, render(d, 120, 44), tc.headline)
		})
	}
}

func TestRemoteFunnelNeverClaimsVerifiedAccessEarly(t *testing.T) {
	t.Parallel()
	s := &FunnelStatus{On: true, Starting: true, Checked: true, Ready: readyTailscale(), Phase: tsfunnel.PhasePanelReady, LocalPort: 49152}
	d := newFunnelDialog(t, "en", s)
	out := render(d, 120, 44)
	require.Contains(t, out, "Phone panel ready; setting up Funnel")
	require.NotContains(t, out, "Phone connected over the internet")

	// The Funnel command succeeded, but no phone has come through yet.
	*s = FunnelStatus{On: true, Checked: true, Ready: readyTailscale(), Phase: tsfunnel.PhaseReady, URL: "https://atlas-pc.example.ts.net", LocalPort: 49152}
	out = render(d, 120, 44)
	require.Contains(t, out, "internet access not verified")
	require.Contains(t, out, "127.0.0.1:49152")
	require.NotContains(t, out, "Phone connected over the internet")
	require.Contains(t, funnelActionIDs(d), rcFunnelPair)
	require.Equal(t, ActionOpenMobile{}, d.run(rcFunnelPair))

	s.Verified, s.Device = time.Date(2026, 1, 2, 15, 4, 5, 0, time.Local), "iPhone"
	out = render(d, 120, 44)
	require.Contains(t, out, "Phone connected over the internet")
	require.Contains(t, out, "iPhone")

	s.Phase, s.Detail = tsfunnel.PhaseLost, "the Funnel process exited."
	out = render(d, 120, 44)
	require.Contains(t, out, "Tailscale Funnel stopped")
	require.Contains(t, funnelActionIDs(d), rcFunnel, "a lost Funnel offers a reconnect")
}

func TestRemoteFunnelApprovalOpensOnlyTailscalePage(t *testing.T) {
	t.Parallel()
	const page = "https://login.tailscale.com/f/funnel?node=abc"
	s := &FunnelStatus{On: true, Starting: true, Checked: true, Ready: readyTailscale(), Phase: tsfunnel.PhaseApproval, ApprovalURL: page}
	d := newFunnelDialog(t, "en", s)
	require.Contains(t, render(d, 120, 44), "Waiting for your approval")
	require.Equal(t, ActionRemoteFunnelOpen{URL: page}, d.run(rcFunnelApprove))

	*s = FunnelStatus{On: true, Checked: true, Ready: readyTailscale(), Err: &tsfunnel.ApprovalRequiredError{URL: page}}
	require.Contains(t, funnelActionIDs(d), rcFunnelApprove)
	require.Equal(t, ActionRemoteFunnelOpen{URL: page}, d.run(rcFunnelApprove))
	require.Equal(t, ActionRemoteFunnelOpen{URL: tsfunnel.AdminDNSPage}, d.run(rcFunnelDNS))
}

func TestRemoteFunnelInstallAsksFirst(t *testing.T) {
	t.Parallel()
	s := &FunnelStatus{On: true, Checked: true, Ready: tsfunnel.Readiness{Problem: tsfunnel.ProblemNotInstalled}}
	d := newFunnelDialog(t, "en", s)
	require.Nil(t, d.run(rcFunnelInstall))
	require.Contains(t, render(d, 120, 44), "SHA-256")
	require.Nil(t, press(d, "n"), "declining installs nothing")
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
