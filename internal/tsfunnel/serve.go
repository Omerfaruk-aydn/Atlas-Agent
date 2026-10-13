
// Occupant describes who uses a public port, for a conflict message.
type Occupant struct {
	Port uint16
	// Foreground is true for a foreground session (another running CLI).
	Foreground bool
	What       string
}

// occupants lists every port in use by background config and by every
// foreground session, including sessions Atlas does not own.
func (sc *serveConfig) occupants() map[uint16]Occupant {
	out := map[uint16]Occupant{}
	add := func(c *serveConfig, fg bool) {
		for k, h := range c.TCP {
			n, _ := strconv.ParseUint(k, 10, 16)
			port := uint16(n)
			if _, seen := out[port]; seen {
				continue
			}
			out[port] = Occupant{Port: port, Foreground: fg, What: c.describe(port, h)}
		}
	}
	add(sc, false)
	for _, id := range slices.Sorted(maps.Keys(sc.Foreground)) {
		if fg := sc.Foreground[id]; fg != nil {
			add(fg, true)
		}
	}
	return out
}

func (sc *serveConfig) describe(port uint16, h *tcpHandler) string {
	if h != nil && h.TCPForward != "" {
		return "tcp → " + Sanitize(h.TCPForward)
	}
	p := strconv.Itoa(int(port))
	for hp, web := range sc.Web {
		if _, wp, err := net.SplitHostPort(hp); err != nil || wp != p || web == nil {
			continue
		}
		for mount, hh := range web.Handlers {
			kind := "serve"
			if sc.AllowFunnel[hp] {
				kind = "funnel"
			}
			switch {
			case hh == nil:
			case hh.Proxy != "":
				return fmt.Sprintf("%s %s → %s", kind, mount, Sanitize(hh.Proxy))
			case hh.Path != "":
				return fmt.Sprintf("%s %s → files", kind, mount)
			default:
				return fmt.Sprintf("%s %s", kind, mount)
			}
		}
	}
	return "in use"
}

// owned finds the foreground session that publishes exactly
// https://host:port/ to target with Funnel turned on.
func (sc *serveConfig) owned(host string, port uint16, target string) (string, bool) {
	hp := net.JoinHostPort(host, strconv.Itoa(int(port)))
	for _, id := range slices.Sorted(maps.Keys(sc.Foreground)) {
		fg := sc.Foreground[id]
		if fg == nil || !fg.AllowFunnel[hp] {
			continue
		}
		web := fg.Web[hp]
		if web == nil {
			continue
		}
		if h := web.Handlers["/"]; h != nil && sameProxy(h.Proxy, target) {
			return id, true
		}
	}
	return "", false
}

// hasSession reports whether a foreground session id still exists.
func (sc *serveConfig) hasSession(id string) bool {
	_, ok := sc.Foreground[id]
	return ok
}

// sameProxy compares the configured proxy with Atlas' target. The CLI
// stores the expanded form, such as http://127.0.0.1:41234.
func sameProxy(got, want string) bool {
	return strings.TrimSuffix(got, "/") == strings.TrimSuffix(want, "/")
}
