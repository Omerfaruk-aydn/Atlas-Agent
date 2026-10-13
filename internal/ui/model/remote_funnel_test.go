func TestFunnelReportsWhatTheUserMustDo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		setup   func(*fakecli.Daemon)
		problem tsfunnel.Problem
	}{
		{"not installed", func(d *fakecli.Daemon) { d.Installed = false }, tsfunnel.ProblemNotInstalled},
		{"service down", func(d *fakecli.Daemon) { d.ServiceDown = true }, tsfunnel.ProblemServiceStopped},
		{"needs login", func(d *fakecli.Daemon) { d.Backend = "NeedsLogin" }, tsfunnel.ProblemNeedsLogin},
		{"no magicdns", func(d *fakecli.Daemon) { d.MagicDNS = false }, tsfunnel.ProblemNeedsMagicDNS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := fakecli.New()
			tc.setup(d)
			m, _ := newFunnelUI(t, d)
			msg := runFunnelCmd(t, m.startFunnel(false)).(funnelStartedMsg)
			m.handleFunnelMsg(msg)
			require.ErrorIs(t, msg.err, errFunnelNotReady)
			s := m.funnelStatus()
			require.True(t, s.On)
			require.True(t, s.Checked)
			require.Equal(t, tc.problem, s.Ready.Problem)
			require.Empty(t, s.URL)
			require.Nil(t, m.devices.gateway)
			require.Empty(t, d.Sessions())
		})
	}
}

func TestFunnelStopDuringApprovalCancelsTheStart(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	d.Funnel = fakecli.FunnelApproval
	m, _ := newFunnelUI(t, d)
	start := m.startFunnel(false)
	progress := m.devices.funnel.progress
	result := make(chan tea.Msg, 1)
	go func() { result <- start() }()
	require.Eventually(t, func() bool {
		phase, url, _ := progress.Snapshot()
		return phase == tsfunnel.PhaseApproval && url == fakecli.ApprovalURL
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, fakecli.ApprovalURL, m.funnelStatus().ApprovalURL)

	stop := m.stopFunnel()
	select {
	case msg := <-result:
		require.Nil(t, msg, "a cancelled start publishes nothing")
	case <-time.After(5 * time.Second):
		t.Fatal("the start did not stop waiting for approval")
	}
	m.handleFunnelMsg(runFunnelCmd(t, stop))
	require.Nil(t, m.devices.gateway)
	require.Empty(t, d.Sessions())
	for _, p := range d.Procs() {
		select {
		case <-p.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("the funnel process kept running")
		}
	}
}

func TestFunnelLateStartResultIsClosed(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	m, _ := newFunnelUI(t, d)
	msg := runFunnelCmd(t, m.startFunnel(false)).(funnelStartedMsg)
	require.NoError(t, msg.err)
	// The user changed the connection before the result arrived.
	m.devices.remote.generation.Add(1)
	cleanup := m.handleFunnelMsg(msg)
	require.Nil(t, m.devices.gateway)
	require.NotNil(t, cleanup)
	cleanup()
	require.Eventually(t, func() bool { return len(d.Sessions()) == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestFunnelAutoStartWritesNoConfiguration(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	m, ws := newFunnelUI(t, d)
	ws.cfg.Tools.Devices.MobileFunnel = true
	cmd := m.autoStartRemote()
	msg := runFunnelCmd(t, cmd).(funnelStartedMsg)
	require.NoError(t, msg.err)
	m.handleFunnelMsg(msg)
	require.NotNil(t, m.devices.gateway)
	require.Empty(t, ws.set)
	require.Empty(t, ws.removed)
	require.False(t, m.dialog.ContainsDialog(dialog.MobileID), "a launch reconnect opens no QR")
}

func TestFunnelSwitchToRelayRemovesTheFunnelFirst(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ATLAS_DEVICES_DIR", dir)
	d := fakecli.New()
	m, ws := newFunnelUI(t, d)
	m.handleFunnelMsg(runFunnelCmd(t, m.startFunnel(false)))
	require.NotNil(t, m.devices.gateway)
	require.Len(t, d.Sessions(), 1)

	enable := m.enableRemote("test-vps")
	require.False(t, m.devices.funnel.on)
	_ = runFunnelCmd(t, enable) // The test has no relay key; only the switch matters.
	require.Empty(t, d.Sessions(), "the old Funnel entry is gone before the relay starts")
	require.Contains(t, ws.removed, funnelConfigKey)
}

func TestFunnelExitRemovesAtlasEntry(t *testing.T) {
	t.Parallel()
	d := fakecli.New()
	m, _ := newFunnelUI(t, d)
	m.handleFunnelMsg(runFunnelCmd(t, m.startFunnel(false)))
	require.Len(t, d.Sessions(), 1)
	m.Close()
	require.Empty(t, d.Sessions())
	_, err := os.Stat(m.devices.funnel.env.StateFile)
	require.ErrorIs(t, err, os.ErrNotExist, "the ownership record goes with the entry")
}
