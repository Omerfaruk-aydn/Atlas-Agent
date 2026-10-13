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
