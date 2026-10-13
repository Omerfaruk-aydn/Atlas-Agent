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
