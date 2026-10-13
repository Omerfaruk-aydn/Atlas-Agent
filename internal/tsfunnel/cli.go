	Identity() procown.Identity
	Done() <-chan struct{}
	Result() (int, error)
	Stop(grace time.Duration)
}

// CLI runs the installed tailscale binary.
type CLI struct {
	// Binary overrides discovery; empty finds the installed CLI.
	Binary string
	// Timeout bounds each short command; zero means CommandTimeout.
	Timeout time.Duration
}

var _ Exec = CLI{}

// Path finds tailscale on PATH, then in the default Windows location.
func (c CLI) Path() (string, error) {
	if c.Binary != "" {
		if _, err := os.Stat(c.Binary); err != nil {
			return "", ErrNotInstalled
		}
		return c.Binary, nil
	}
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles", "ProgramW6432"} {
			if dir := os.Getenv(env); dir != "" {
				p := filepath.Join(dir, "Tailscale", "tailscale.exe")
				if _, err := os.Stat(p); err == nil {
					return p, nil
				}
			}
		}
	}
	return "", ErrNotInstalled
}

// Output runs a short command, bounded by CommandTimeout and ctx.
func (c CLI) Output(ctx context.Context, args ...string) ([]byte, error) {
	path, err := c.Path()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, orDefault(c.Timeout, CommandTimeout))
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	hideWindow(cmd)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr limitedBuffer
	stdout.max, stderr.max = maxOutput, 16<<10
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, ErrTimeout
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case stdout.overflow:
		return nil, fmt.Errorf("%w: output too large", ErrMalformed)
	case err != nil:
		return stdout.Bytes(), classify(err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// classify turns a failed CLI run into an error the dialog can explain.
// The CLI's own words are kept, shortened, as detail.
func classify(err error, stderr string) error {
	detail := Sanitize(stderr)
	lower := strings.ToLower(stderr)
	for _, hint := range []string{"failed to connect to local tailscale", "is tailscale running", "tailscaled not running", "the system cannot find the file specified", "connect: connection refused", "no such file or directory"} {
		if strings.Contains(lower, hint) {
			return fmt.Errorf("%w: %s", ErrServiceUnavailable, detail)
		}
	}
	if detail == "" {
		return err
	}
	return fmt.Errorf("%s: %w", detail, err)
}

// Start launches a long-lived command as an owned process.
func (c CLI) Start(args []string, onLine func(string)) (Process, error) {
	path, err := c.Path()
	if err != nil {
		return nil, err
	}
	r, w := io.Pipe()
	p, err := procown.Start(procown.Spec{Argv: append([]string{path}, args...), Stdout: w, Stderr: w})
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 4096), 64<<10)
		for sc.Scan() {
			onLine(sc.Text())
		}
		_, _ = io.Copy(io.Discard, r)
	}()
	go func() {
		<-p.Done()
		_ = w.Close()
	}()
	return ownedProcess{p}, nil
}

type ownedProcess struct{ p *procown.Proc }

func (o ownedProcess) Identity() procown.Identity { return o.p.Identity }
func (o ownedProcess) Done() <-chan struct{}      { return o.p.Done() }
func (o ownedProcess) Result() (int, error)       { return o.p.Result() }
func (o ownedProcess) Stop(grace time.Duration)   { o.p.Stop(grace) }

// limitedBuffer keeps at most max bytes and records an overflow.
type limitedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	max      int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - b.buf.Len(); len(p) > room {
		b.overflow = true
		b.buf.Write(p[:max(0, room)])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

func (b *limitedBuffer) String() string { return string(b.Bytes()) }

// Sanitize shortens CLI text for display and logs. It drops control
// characters and anything that looks like an auth key; Atlas never passes
// one, but the user's environment might.
func Sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.HasPrefix(f, "tskey-") || strings.Contains(f, "--auth-key") || strings.Contains(f, "--authkey") {
			fields[i] = "[redacted]"
		}
	}
	s = strings.Join(fields, " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
