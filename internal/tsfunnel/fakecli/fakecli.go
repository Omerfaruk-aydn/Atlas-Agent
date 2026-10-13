		st["Self"] = map[string]any{"DNSName": ""}
		st["CurrentTailnet"] = nil
	}
	b, _ := json.Marshal(st)
	return b
}

func (d *Daemon) serveJSONLocked() []byte {
	if d.ServeRaw != nil {
		return d.ServeRaw
	}
	if len(d.background) == 0 && len(d.foreground) == 0 {
		return []byte("null\n")
	}
	cfg := maps.Clone(d.background)
	if len(d.foreground) > 0 {
		fg := map[string]any{}
		for k, v := range d.foreground {
			fg[k] = v
		}
		cfg["Foreground"] = fg
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return b
}

func entry(host string, port uint16, target string) map[string]any {
	hp := net.JoinHostPort(host, strconv.Itoa(int(port)))
	return map[string]any{
		"TCP":         map[string]any{strconv.Itoa(int(port)): map[string]any{"HTTPS": true}},
		"Web":         map[string]any{hp: map[string]any{"Handlers": map[string]any{"/": map[string]any{"Proxy": target}}}},
		"AllowFunnel": map[string]any{hp: true},
	}
}

// portUsedLocked mirrors tailscaled: a new foreground listener must use
// a port no background or foreground config uses.
func (d *Daemon) portUsedLocked(port string) bool {
	used := func(cfg map[string]any) bool {
		tcp, _ := cfg["TCP"].(map[string]any)
		_, ok := tcp[port]
		return ok
	}
	if used(d.background) {
		return true
	}
	for _, fg := range d.foreground {
		if used(fg) {
			return true
		}
	}
	return false
}

// Proc is a fake long-lived CLI process.
type Proc struct {
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	code   int
	Args   []string
	onStop func()
}

func newProc(args []string) *Proc { return &Proc{done: make(chan struct{}), code: -1, Args: args} }

// Identity implements tsfunnel.Process; fakes never match a real process.
func (p *Proc) Identity() procown.Identity { return procown.Identity{} }

// Done implements tsfunnel.Process.
func (p *Proc) Done() <-chan struct{} { return p.done }

// Result implements tsfunnel.Process.
func (p *Proc) Result() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code, nil
}

// Stop implements tsfunnel.Process.
func (p *Proc) Stop(time.Duration) { p.exit(1) }

// Crash ends the process as if it died on its own.
func (p *Proc) Crash() { p.exit(2) }

func (p *Proc) exit(code int) {
	p.once.Do(func() {
		p.mu.Lock()
		p.code = code
		stop := p.onStop
		p.mu.Unlock()
		if stop != nil {
			stop()
		}
		close(p.done)
	})
}

// Procs returns the started processes.
func (d *Daemon) Procs() []*Proc {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.procs)
}

// Start implements tsfunnel.Exec.
func (d *Daemon) Start(args []string, onLine func(string)) (tsfunnel.Process, error) {
	d.mu.Lock()
	d.calls = append(d.calls, slices.Clone(args))
	if !d.Installed {
		d.mu.Unlock()
		return nil, tsfunnel.ErrNotInstalled
	}
	p := newProc(slices.Clone(args))
	d.procs = append(d.procs, p)
	d.mu.Unlock()
	switch {
	case len(args) == 1 && args[0] == "up":
		go d.runUp(p, onLine)
	case len(args) == 3 && args[0] == "funnel" && strings.HasPrefix(args[1], "--https="):
		go d.runFunnel(p, strings.TrimPrefix(args[1], "--https="), args[2], onLine)
	default:
		onLine("fake tailscale: unexpected command")
		p.exit(2)
	}
	return p, nil
}

func (d *Daemon) runUp(p *Proc, onLine func(string)) {
	d.mu.Lock()
	fail := d.LoginFails
	d.mu.Unlock()
	if fail != "" {
		onLine(fail)
		p.exit(1)
		return
	}
	onLine("")
	onLine("To authenticate, visit:")
	onLine("")
	onLine("\t" + LoginURL)
	select {
	case <-d.login:
		d.mu.Lock()
		d.Backend = "Running"
		d.mu.Unlock()
		onLine("Success.")
		p.exit(0)
	case <-p.done:
	}
}

func (d *Daemon) runFunnel(p *Proc, port, target string, onLine func(string)) {
	d.mu.Lock()
	mode := d.Funnel
	d.mu.Unlock()
	switch mode {
	case FunnelFail:
		onLine("error: funnel not available")
		p.exit(1)
		return
	case FunnelApprovalExit:
		onLine("Funnel is not enabled on your tailnet.")
		onLine("To enable, visit:")
		onLine("         " + ApprovalURL)
		p.exit(0)
		return
	case FunnelHang:
		<-p.done
		return
	case FunnelApproval:
		onLine("Funnel is not enabled on your tailnet.")
		onLine("To enable, visit:")
		onLine("         " + ApprovalURL)
		select {
		case <-d.approve:
		case <-p.done:
			return
		}
	}
	n, _ := strconv.ParseUint(port, 10, 16)
	d.mu.Lock()
	select {
	case <-p.done:
		d.mu.Unlock()
		return
	default:
	}
	if d.portUsedLocked(port) {
		d.mu.Unlock()
		onLine("listener already exists for port " + port)
		p.exit(1)
		return
	}
	d.nextID++
	id := fmt.Sprintf("session-%d", d.nextID)
	host := strings.TrimSuffix(d.Host, ".")
	d.foreground[id] = entry(host, uint16(n), target)
	keep := d.KeepSessions
	p.mu.Lock()
	p.onStop = func() {
		if keep {
			return
		}
		d.mu.Lock()
		delete(d.foreground, id)
		d.mu.Unlock()
	}
	p.mu.Unlock()
	d.mu.Unlock()
	onLine("Available on the internet:")
	onLine("")
	onLine("https://" + host)
	onLine("|-- / proxy " + target)
	onLine("")
	onLine("Press Ctrl+C to exit.")
	<-p.done
}
