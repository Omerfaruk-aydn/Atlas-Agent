	}
	if !f.Ready.Ready() {
		return d.funnelProblem(f.Ready)
	}
	if f.Err != nil {
		return rcStateError, bad, remotecontrol.Msg("The Tailscale connection failed: %s", f.Err.Error())
	}
	return "Tailscale is ready", good, remotecontrol.Msg("Choose Reconnect with Tailscale to publish the phone panel.")
}

func (d *Remote) funnelProblem(r tsfunnel.Readiness) (string, lipgloss.Style, remotecontrol.Message) {
	bad, accent := d.com.Styles.Dialog.OAuth.ErrorText, d.com.Styles.Dialog.TitleAccent
	switch r.Problem {
	case tsfunnel.ProblemNotInstalled:
		return "Tailscale is not installed", accent, remotecontrol.Msg("Install Tailscale, then sign in to your own account. No VPS, domain or router setting is needed.")
	case tsfunnel.ProblemServiceStopped:
		return "The Tailscale service is not running", accent, remotecontrol.Msg("Open the Tailscale app; it starts the service.")
	case tsfunnel.ProblemOldVersion:
		return "Tailscale is too old", accent, remotecontrol.Msg("Atlas needs Tailscale %s or newer. Update it, then check again.", tsfunnel.MinVersion)
	case tsfunnel.ProblemNeedsLogin:
		return "Tailscale sign-in needed", accent, remotecontrol.Msg("Sign in to your own Tailscale account in the browser.")
	case tsfunnel.ProblemMachineAuth:
		return "Waiting for device approval", accent, remotecontrol.Msg("An admin of your tailnet must approve this computer in the Tailscale admin console.")
	case tsfunnel.ProblemDisconnected:
		return "Tailscale is disconnected", accent, remotecontrol.Msg("Sign in to Tailscale to connect it again.")
	case tsfunnel.ProblemStarting:
		return "Tailscale is starting", accent, remotecontrol.Msg("Wait a moment, then check again.")
	case tsfunnel.ProblemNeedsMagicDNS:
		return "MagicDNS and HTTPS are needed", accent, remotecontrol.Msg("Turn on MagicDNS and HTTPS certificates in the Tailscale admin console, then check again.")
	case tsfunnel.ProblemNoFunnelPort:
		return "Funnel is not allowed on any port", bad, remotecontrol.Msg("Your tailnet policy allows Funnel on none of 443, 8443 or 10000.")
	}
	return rcStateError, bad, remotecontrol.Msg("Tailscale did not answer as expected: %s", r.Detail)
}

func installStepText(step string) string {
	switch tsfunnel.InstallStep(step) {
	case tsfunnel.StepDownload:
		return "Downloading the official installer…"
	case tsfunnel.StepVerify:
		return "Checking the SHA-256 and the Tailscale signature…"
	case tsfunnel.StepInstall:
		return "Follow the Tailscale installer; Windows asks for approval."
	}
	return "Reading Tailscale's package list…"
}

// funnelLines are the status lines below the headline in Tailscale mode.
func (d *Remote) funnelLines(width int) []string {
	t := d.com.Styles
	quiet := t.Dialog.SecondaryText.Padding(0)
	f := d.funnel()
	var lines []string
	if r := f.Ready; r.CLIPath != "" {
		parts := []string{orDash(r.Version), orDash(r.Backend)}
		if r.Host != "" {
			parts = append(parts, r.Host)
		}
		lines = append(lines, quiet.Render(fmt.Sprintf(d.com.Text("Tailscale: %s"), strings.Join(parts, " · "))))
	}
	if f.URL != "" {
		lines = append(lines, quiet.Render(fmt.Sprintf(d.com.Text("Phone address: %s"), f.URL)))
	}
	if f.LocalPort > 0 {
		lines = append(lines, quiet.Render(fmt.Sprintf(d.com.Text("Phone panel: only on this computer (127.0.0.1:%d)"), f.LocalPort)))
	}
	if gw := d.gateway(); gw != nil && gw.UsesFunnel() {
		lines = append(lines, d.phoneCountLine())
	}
	lines = append(lines, wrapLines(quiet, d.com.Text("Keep this computer on and awake. Tailscale Funnel has bandwidth limits and serves only ts.net addresses on ports 443, 8443 and 10000."), width)...)
	return lines
}

// funnelDiagnostics adds the checked Tailscale facts to Diagnostics.
func (d *Remote) funnelDiagnostics() []string {
	f := d.funnel()
	if !f.On && !f.Checked {
		return nil
	}
	t := d.com.Styles
	kv := func(k, v string) string { return t.Dialog.SecondaryText.Padding(0).Render(d.com.Text(k) + ": " + v) }
	r := f.Ready
	var ports []string
	for _, p := range r.FunnelPorts {
		ports = append(ports, fmt.Sprint(p))
	}
	lines := []string{
		"", t.Dialog.PrimaryText.Padding(0).Render(d.com.Text("Tailscale")),
		kv("CLI", orDash(r.CLIPath)),
		kv("Version", orDash(r.Version)),
		kv("State", orDash(r.Backend)),
		kv("Device name", orDash(r.Host)),
		kv("MagicDNS", fmt.Sprint(r.MagicDNS)),
		kv("HTTPS certificates", fmt.Sprint(r.HTTPS)),
		kv("Funnel allowed", fmt.Sprint(r.Funnel)),
		kv("Funnel ports", orDash(strings.Join(ports, ", "))),
		kv("Phase", orDash(string(f.Phase))),
	}
	if r.Detail != "" {
		lines = append(lines, kv("Detail", r.Detail))
	}
	if f.Detail != "" {
		lines = append(lines, kv("Funnel", f.Detail))
	}
	if !f.Seen.IsZero() {
		lines = append(lines, kv("Last internet request", f.Seen.Format("15:04:05")))
	}
	return lines
}
