		return util.ReportError(fmt.Errorf("%s", m.com.Text("Only Tailscale's own pages are opened")))
	}
	open := m.funnelEnv().OpenURL
	return func() tea.Msg {
		if open == nil {
			return nil
		}
		if err := open(url); err != nil {
			return util.NewErrorMsg(fmt.Errorf("%s %s: %w", m.com.Text("Open this address in a browser:"), url, err))
		}
		return nil
	}
}

// funnelStatus is the dialog's view of the Tailscale mode.
func (m *UI) funnelStatus() dialog.FunnelStatus {
	f := &m.devices.funnel
	s := dialog.FunnelStatus{
		On: f.on, Starting: f.on && m.devices.remote.starting, Checking: f.checking,
		Task: f.task, Ready: f.ready, Checked: f.checked, Err: f.err,
	}
	if p := f.step.Load(); p != nil {
		s.InstallStep = *p
	}
	s.Phase, s.ApprovalURL, s.Detail = f.progress.Snapshot()
	if gw := m.devices.gateway; gw != nil && gw.UsesFunnel() {
		s.URL = gw.FunnelURL()
		s.LocalPort = gw.FunnelLocalPort()
		s.Seen = gw.FunnelSeen()
		s.Verified, s.Device = gw.FunnelVerified()
	}
	return s
}

func (m *UI) handleFunnelMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case funnelStartedMsg:
		return m.handleFunnelStarted(msg)
	case funnelCheckedMsg:
		return m.handleFunnelChecked(msg)
	case funnelTaskMsg:
		return m.handleFunnelTask(msg)
	case funnelStoppedMsg:
		return m.handleFunnelStopped(msg)
	}
	return nil
}

// closeFunnel removes Atlas' Funnel entry when the TUI exits. The Funnel
// process would end with Atlas anyway; closing first also removes the
// ownership record. The Tailscale service keeps running.
func (m *UI) closeFunnel() {
	gw := m.devices.remote.attached.Load()
	if gw == nil || !gw.UsesFunnel() {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = gw.Close()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}
