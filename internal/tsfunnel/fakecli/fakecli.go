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
