		r.Tailnet = st.CurrentTailnet.Name
		r.MagicDNS = st.CurrentTailnet.MagicDNSEnabled
	}
	host := strings.ToLower(strings.TrimSuffix(st.Self.DNSName, "."))
	if !r.MagicDNS || host == "" {
		r.Problem = ProblemNeedsMagicDNS
		return r
	}
	if err := checkFunnelHost(host); err != nil {
		r.Problem, r.Detail = ProblemNeedsMagicDNS, err.Error()
		return r
	}
	r.Host = host
	r.HTTPS = st.Self.hasCap(capHTTPS)
	r.Funnel = st.Self.hasCap(capFunnel)
	r.FunnelPorts = slices.Clone(FunnelPorts)
	if r.Funnel {
		if ports, ok := funnelPorts(st.Self); ok {
			r.FunnelPorts = ports
		}
		if len(r.FunnelPorts) == 0 {
			r.Problem, r.Detail = ProblemNoFunnelPort, "the tailnet policy allows none of 443, 8443 or 10000"
		}
	}
	return r
}

// funnelPorts reads the funnel-ports capability, whose ports travel in the
// key's query string (https://tailscale.com/cap/funnel-ports?ports=443,8443).
func funnelPorts(self *peerJSON) ([]uint16, bool) {
	for k := range self.CapMap {
		if !strings.HasPrefix(k, capFunnelPorts+"?") {
			continue
		}
		u, err := url.Parse(k)
		if err != nil {
			return nil, true
		}
		allowed := map[uint16]bool{}
		for part := range strings.SplitSeq(u.Query().Get("ports"), ",") {
			lo, hi, isRange := strings.Cut(strings.TrimSpace(part), "-")
			if !isRange {
				hi = lo
			}
			a, err1 := strconv.ParseUint(lo, 10, 16)
			b, err2 := strconv.ParseUint(hi, 10, 16)
			if err1 != nil || err2 != nil || a > b {
				continue
			}
			for _, p := range FunnelPorts {
				if uint64(p) >= a && uint64(p) <= b {
					allowed[p] = true
				}
			}
		}
		var out []uint16
		for _, p := range FunnelPorts {
			if allowed[p] {
				out = append(out, p)
			}
		}
		return out, true
	}
	return nil, false
}

// checkFunnelHost accepts only a MagicDNS name in a ts.net tailnet, the
// only domain Funnel serves.
func checkFunnelHost(h string) error {
	if !strings.HasSuffix(h, ".ts.net") || len(h) > 253 {
		return fmt.Errorf("device name %q is not a ts.net MagicDNS name", Sanitize(h))
	}
	for label := range strings.SplitSeq(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("device name %q is not a valid DNS name", Sanitize(h))
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return fmt.Errorf("device name %q is not a valid DNS name", Sanitize(h))
			}
		}
	}
	return nil
}

// loginURL keeps only an https URL on Tailscale's login host, so a
// manipulated CLI output cannot make Atlas open an arbitrary page.
func loginURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return ""
	}
	if h := strings.ToLower(u.Hostname()); h != "login.tailscale.com" {
		return ""
	}
	return u.String()
}

// AdminDNSPage is where MagicDNS and HTTPS certificates are turned on.
const AdminDNSPage = "https://login.tailscale.com/admin/dns"

// OfficialPage reports whether raw is a Tailscale page Atlas may open in
// the browser: sign-in and admin pages, or the public download pages.
func OfficialPage(raw string) bool {
	if loginURL(raw) != "" {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && strings.ToLower(u.Hostname()) == "tailscale.com"
}

// versionAtLeast compares the leading x.y.z of a tailscaled version such
// as "1.104.1-t1234abcd-g5678".
func versionAtLeast(v, minimum string) bool {
	a, ok1 := parseVersion(v)
	b, ok2 := parseVersion(minimum)
	if !ok1 || !ok2 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v, _, _ = strings.Cut(strings.TrimSpace(v), "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
