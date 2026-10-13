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
