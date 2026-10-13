	if _, ok := parseVersion(meta.MSIsVersion); !ok || !pattern.MatchString(name) {
		return "", fmt.Errorf("%w: package index lists %q", ErrMalformed, Sanitize(name))
	}
	return name, nil
}

func fetch(ctx context.Context, client *http.Client, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: response too large", u)
	}
	return data, nil
}

func download(ctx context.Context, client *http.Client, u, path string, limit int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if n > limit {
		return "", errors.New("download: file larger than expected")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SignedByTailscale accepts a certificate subject whose CN or O is
// exactly "Tailscale Inc.".
func SignedByTailscale(subject string) bool {
	for _, rdn := range splitRDN(subject) {
		k, v, ok := strings.Cut(rdn, "=")
		if !ok {
			continue
		}
		k = strings.ToUpper(strings.TrimSpace(k))
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if (k == "CN" || k == "O") && v == "Tailscale Inc." {
			return true
		}
	}
	return false
}

// splitRDN splits a distinguished name at commas outside quotes.
func splitRDN(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			cur.WriteRune(r)
		case r == ',' && !quoted:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(out, cur.String())
}

func orString(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orInt(n, def int64) int64 {
	if n <= 0 {
		return def
	}
	return n
}
