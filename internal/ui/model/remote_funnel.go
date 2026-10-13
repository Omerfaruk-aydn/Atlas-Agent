	m.devices.homeLAN = false
	r.profileID, r.starting, r.startErr = "", true, nil
	if !auto {
		r.retry = 0
	}
	f.on, f.err, f.checking = true, nil, false
	progress := tsfunnel.NewProgress()
	f.progress = progress
	ws := m.com.Workspace
	relayOn := false
	if cfg := m.com.Config(); cfg != nil {
		relayOn = cfg.Tools.Devices.MobileRelayURL != "" || cfg.Tools.Devices.MobileRelayTokenFile != ""
	}
	opts := m.funnelOptions()
	return func() tea.Msg {
		r.lifecycle.Lock()
		defer r.lifecycle.Unlock()
		if old != nil {
			_ = old.Close()
		}
		if generation != r.generation.Load() {
			return nil
		}
		fail := func(ready tsfunnel.Readiness, err error) tea.Msg {
			progress.Set(tsfunnel.PhaseFailed)
			return funnelStartedMsg{generation: generation, ready: ready, err: err, auto: auto}
		}
		if !auto {
			// Only one public route at a time: the VPS relay is turned off
			// on this computer; its profile and server stay as they are.
			if err := ws.SetConfigField(config.ScopeGlobal, funnelConfigKey, true); err != nil {
				return fail(tsfunnel.Readiness{}, err)
			}
			if relayOn {
				if err := errors.Join(
					ws.RemoveConfigField(config.ScopeGlobal, "tools.devices.mobile_relay_url"),
					ws.RemoveConfigField(config.ScopeGlobal, "tools.devices.mobile_relay_token_file"),
				); err != nil {
					return fail(tsfunnel.Readiness{}, err)
				}
			}
		}
		ctx, cancel := r.generationContext(generation)
		defer cancel()
		ready := tsfunnel.Inspect(ctx, env.CLI, env.Service)
		if !ready.Ready() {
			return fail(ready, errFunnelNotReady)
		}
		mgr := env.manager()
		plan, err := mgr.Prepare(ctx, ready)
		if err != nil {
			return fail(ready, err)
		}
		progress.Set(tsfunnel.PhasePanel)
		gw, err := env.Gateway(opts)
		if err != nil {
			return fail(ready, err)
		}
		var mu sync.Mutex
		var link *tsfunnel.Link
		port, err := gw.ServeFunnel(ctx, plan.HostHeader(), func() error {
			mu.Lock()
			l := link
			mu.Unlock()
			if l == nil {
				return nil
			}
			return l.Close()
		})
		if err != nil {
			_ = gw.Close()
			return fail(ready, err)
		}
		progress.Set(tsfunnel.PhasePanelReady)
		l, err := mgr.Start(ctx, plan, port, progress)
		if err != nil {
			_ = gw.Close()
			if generation != r.generation.Load() {
				return nil
			}
			return fail(ready, err)
		}
		mu.Lock()
		link = l
		mu.Unlock()
		if generation != r.generation.Load() {
			_ = gw.Close()
			return nil
		}
		return funnelStartedMsg{generation: generation, gw: gw, ready: ready, auto: auto}
	}
}

func (m *UI) handleFunnelStarted(msg funnelStartedMsg) tea.Cmd {
	r := &m.devices.remote
	f := &m.devices.funnel
	if msg.generation != r.generation.Load() {
		return closeGatewayCmd(msg.gw)
	}
	r.starting = false
	f.ready, f.checked = msg.ready, msg.ready.Problem != "" || msg.ready.Host != ""
	if msg.err != nil {
		f.err, r.startErr = msg.err, msg.err
		if msg.auto {
			return m.scheduleRemoteRetry()
		}
		return nil
	}
	f.err, r.startErr, r.retry = nil, nil, 0
	m.setGateway(msg.gw)
	if msg.auto {
		return nil
	}
	if m.dialog.ContainsDialog(dialog.MobileID) {
		return nil
	}
	d := dialog.NewMobile(m.com, msg.gw)
	m.dialog.OpenDialog(d)
	return tea.Batch(d.Init(), util.ReportInfo(m.com.Text("Tailscale Funnel is set up; pair a phone to verify internet access")))
}

// stopFunnel closes the gateway, which removes Atlas' Funnel entry and
// waits until Tailscale no longer lists it, then forgets the choice.
func (m *UI) stopFunnel() tea.Cmd {
	r := &m.devices.remote
	f := &m.devices.funnel
	generation := r.generation.Add(1)
	gw := m.devices.gateway
	m.setGateway(nil)
	m.dialog.CloseDialog(dialog.MobileID)
	r.starting, r.startErr, r.retry = false, nil, 0
	f.on, f.err, f.task = false, nil, ""
	ws := m.com.Workspace
	return func() tea.Msg {
		r.lifecycle.Lock()
		defer r.lifecycle.Unlock()
		var closeErr error
		if gw != nil {
			closeErr = gw.Close()
		}
		if generation != r.generation.Load() {
			return nil
		}
		err := ws.RemoveConfigField(config.ScopeGlobal, funnelConfigKey)
		return funnelStoppedMsg{generation: generation, err: errors.Join(closeErr, err)}
	}
}

func (m *UI) handleFunnelStopped(msg funnelStoppedMsg) tea.Cmd {
	if msg.generation != m.devices.remote.generation.Load() {
		return nil
	}
	d := m.remoteDialog()
	switch {
	case errors.Is(msg.err, tsfunnel.ErrResidual):
		if d != nil {
			d.SetNotice(remotecontrol.Msg("Tailscale still lists Atlas' Funnel entry. Restart the Tailscale app or remove it with the Tailscale CLI; Atlas does not remove entries by force."), false)
		}
		return util.ReportError(msg.err)
	case msg.err != nil:
		return util.ReportError(msg.err)
	}
	if d != nil {
		d.SetNotice(remotecontrol.Msg("Tailscale connection stopped. Tailscale no longer lists Atlas' Funnel entry, so the address no longer reaches this computer."), true)
	}
	return util.ReportInfo(m.com.Text("Tailscale connection stopped"))
}

// checkFunnel only reads the Tailscale state.
func (m *UI) checkFunnel() tea.Cmd {
	f := &m.devices.funnel
	if f.checking {
		return nil
	}
	f.checking = true
	env := m.funnelEnv()
	generation := m.devices.remote.generation.Load()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), tsfunnel.CommandTimeout+5*time.Second)
		defer cancel()
		return funnelCheckedMsg{generation: generation, ready: tsfunnel.Inspect(ctx, env.CLI, env.Service)}
	}
}

func (m *UI) handleFunnelChecked(msg funnelCheckedMsg) tea.Cmd {
	f := &m.devices.funnel
	f.checking = false
	if msg.generation != m.devices.remote.generation.Load() {
		return nil
	}
	f.ready, f.checked = msg.ready, true
	// A check after a fix continues the connection the user asked for.
	if f.on && msg.ready.Ready() && m.devices.gateway == nil && !m.devices.remote.starting {
		return m.startFunnel(false)
	}
	return nil
}

// runFunnelTask runs a helper the user chose in /rc. Install and app
// changes are made only by the official installer and the Tailscale app;
// sign-in happens on Tailscale's own page.
func (m *UI) runFunnelTask(task string) tea.Cmd {
	f := &m.devices.funnel
	if f.task != "" {
		return nil
	}
	env := m.funnelEnv()
	r := &m.devices.remote
	generation := r.generation.Load()
	f.task, f.err = task, nil
	f.step.Store(nil)
	switch task {
	case funnelTaskInstall:
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
