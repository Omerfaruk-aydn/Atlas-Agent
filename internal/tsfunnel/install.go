		}
		return nil
	}
	client = &c
	arch := msiArch(orString(in.Arch, runtime.GOARCH))
	if arch == "" {
		return fmt.Errorf("no Tailscale MSI for %s", runtime.GOARCH)
	}
	step(StepMetadata)
	name, err := in.pickMSI(ctx, client, base, arch)
	if err != nil {
		return err
	}
	sumURL := base.ResolveReference(&url.URL{Path: name + ".sha256"})
	sumText, err := fetch(ctx, client, sumURL.String(), 1024)
	if err != nil {
		return fmt.Errorf("read the published SHA-256: %w", err)
	}
	var want string
	if f := strings.Fields(string(sumText)); len(f) > 0 {
		want = strings.ToLower(f[0])
	}
	if !hexSHA256.MatchString(want) {
		return errors.New("the published SHA-256 is not a SHA-256 value")
	}
	dir, err := os.MkdirTemp(in.TempRoot, "atlas-tailscale-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, name)
	step(StepDownload)
	got, err := download(ctx, client, base.ResolveReference(&url.URL{Path: name}).String(), path, orInt(in.MaxBytes, 300<<20))
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the download does not match the published SHA-256 (got %s, want %s); nothing was installed", got[:12], want[:12])
	}
	step(StepVerify)
	sig, err := in.Verify(ctx, path)
	if err != nil {
		return fmt.Errorf("check the installer signature: %w", err)
	}
	if sig.Status != "Valid" {
		return fmt.Errorf("the installer signature is %q, not Valid; nothing was installed", Sanitize(sig.Status))
	}
	if !SignedByTailscale(sig.Subject) {
		return fmt.Errorf("the installer is signed by %q, not Tailscale Inc.; nothing was installed", Sanitize(sig.Subject))
	}
	step(StepInstall)
	return in.Run(ctx, path)
}

func (in Installer) pickMSI(ctx context.Context, client *http.Client, base *url.URL, arch string) (string, error) {
	index := *base
	index.RawQuery = "mode=json"
	data, err := fetch(ctx, client, index.String(), 1<<20)
	if err != nil {
		return "", fmt.Errorf("read the package index: %w", err)
	}
	var meta struct {
		MSIs        map[string]string
		MSIsVersion string
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("%w: package index", ErrMalformed)
	}
	name := meta.MSIs[arch]
	// The name becomes a path and a URL; accept only the expected shape.
	pattern := regexp.MustCompile(`^tailscale-setup-` + regexp.QuoteMeta(meta.MSIsVersion) + `-` + regexp.QuoteMeta(arch) + `\.msi$`)
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
