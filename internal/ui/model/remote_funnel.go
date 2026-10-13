		return func() tea.Msg {
			if env.Install == nil {
				return funnelTaskMsg{generation: generation, task: task, err: errors.New("installation is not available here")}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			err := env.Install(ctx, func(s tsfunnel.InstallStep) {
				step := string(s)
				f.step.Store(&step)
			})
			return funnelTaskMsg{generation: generation, task: task, err: err}
		}
	case funnelTaskLogin:
		progress := tsfunnel.NewProgress()
		f.progress = progress
		return func() tea.Msg {
			ctx, cancel := r.generationContext(generation)
			defer cancel()
			err := tsfunnel.Login(ctx, env.CLI, progress, env.OpenURL)
			return funnelTaskMsg{generation: generation, task: task, err: err}
		}
	case funnelTaskApp:
		return func() tea.Msg {
			if env.OpenApp == nil {
				return funnelTaskMsg{generation: generation, task: task, err: tsfunnel.ErrNotInstalled}
			}
			if err := env.OpenApp(); err != nil {
				return funnelTaskMsg{generation: generation, task: task, err: err}
			}
			// The app starts the service; wait briefly for it to answer.
			deadline := time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) {
				ctx, cancel := context.WithTimeout(context.Background(), tsfunnel.CommandTimeout)
				ready := tsfunnel.Inspect(ctx, env.CLI, env.Service)
				cancel()
				if ready.Problem != tsfunnel.ProblemServiceStopped && ready.Problem != tsfunnel.ProblemStarting {
					break
				}
				time.Sleep(time.Second)
			}
			return funnelTaskMsg{generation: generation, task: task}
		}
	}
	f.task = ""
	return nil
}

func (m *UI) handleFunnelTask(msg funnelTaskMsg) tea.Cmd {
	f := &m.devices.funnel
	f.task = ""
	f.step.Store(nil)
	if msg.err != nil {
		if msg.generation == m.devices.remote.generation.Load() {
			f.err = msg.err
		}
		if errors.Is(msg.err, context.Canceled) {
			return nil
		}
		return util.ReportError(fmt.Errorf("%s: %w", m.com.Text("Tailscale"), msg.err))
	}
	if msg.generation != m.devices.remote.generation.Load() {
		return nil
	}
	return m.checkFunnel()
}

// openFunnelPage opens a Tailscale page the user asked for. Only
// Tailscale's own sign-in, approval, admin and download pages qualify.
func (m *UI) openFunnelPage(url string) tea.Cmd {
	if !tsfunnel.OfficialPage(url) {
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
