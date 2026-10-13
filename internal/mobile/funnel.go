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
