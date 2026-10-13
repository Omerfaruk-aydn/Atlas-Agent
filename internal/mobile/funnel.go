		_ = ft.ln.Close()
	})
	return ft.err
}

// markVerified records an authenticated phone request through Funnel.
func (ft *funnelTarget) markVerified(d Device) {
	name := d.Name
	ft.device.Store(&name)
	ft.verified.Store(time.Now().UnixMilli())
}

// UsesFunnel reports whether phones reach this gateway through Funnel.
func (g *Gateway) UsesFunnel() bool { return g.funnel.Load() != nil }

// FunnelURL is the public Funnel address, or "".
func (g *Gateway) FunnelURL() string {
	if ft := g.funnel.Load(); ft != nil {
		return "https://" + ft.host
	}
	return ""
}

// FunnelLocalPort is the loopback port Funnel forwards to, or 0.
func (g *Gateway) FunnelLocalPort() int {
	if ft := g.funnel.Load(); ft != nil {
		return ft.ln.Addr().(*net.TCPAddr).Port
	}
	return 0
}

// FunnelSeen is the time of the last request that tailscaled marked as
// coming from the internet, or zero.
func (g *Gateway) FunnelSeen() time.Time {
	return funnelTime(g, func(ft *funnelTarget) int64 { return ft.seen.Load() })
}

// FunnelVerified is the time of the last request from a paired phone
// through Funnel, and that phone's name. Only this proves that a phone
// reached the computer over the internet.
func (g *Gateway) FunnelVerified() (time.Time, string) {
	ft := g.funnel.Load()
	if ft == nil {
		return time.Time{}, ""
	}
	at := funnelTime(g, func(ft *funnelTarget) int64 { return ft.verified.Load() })
	if p := ft.device.Load(); p != nil {
		return at, *p
	}
	return at, ""
}

func funnelTime(g *Gateway, field func(*funnelTarget) int64) time.Time {
	ft := g.funnel.Load()
	if ft == nil {
		return time.Time{}
	}
	if ms := field(ft); ms > 0 {
		return time.UnixMilli(ms)
	}
	return time.Time{}
}

// forwardedClient returns the last X-Forwarded-For address, which
// tailscaled sets to the connecting client.
func forwardedClient(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return ""
	}
	parts := strings.Split(xff, ",")
	ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1]))
	if ip == nil {
		return ""
	}
	return ip.String()
}

// checkFunnelHost accepts only a ts.net name, optionally with one of the
// ports Funnel serves. Funnel never serves any other domain.
func checkFunnelHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	name := host
	if h, p, err := net.SplitHostPort(host); err == nil {
		n, perr := strconv.Atoi(p)
		if perr != nil || (n != 8443 && n != 10000) {
			return "", fmt.Errorf("funnel host %q has an unsupported port", host)
		}
		name = h
	}
	if !strings.HasSuffix(name, ".ts.net") || strings.Count(name, ".") < 3 {
		return "", fmt.Errorf("funnel host %q is not a ts.net device name", host)
	}
	if err := checkHostname(name); err != nil {
		return "", err
	}
	return host, nil
}
