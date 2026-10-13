		_ = procown.KillOwned(rec.Process)
	}
	gone, err := m.waitGone(ctx, rec.Session)
	if err != nil {
		return err
	}
	if !gone {
		return fmt.Errorf("%w (%s, session %s); restart Tailscale or remove it with the Tailscale CLI", ErrResidual, Plan{Host: rec.Host, Port: rec.Port}.URL(), Sanitize(rec.Session))
	}
	return m.removeRecord()
}

func (m *Manager) waitGone(ctx context.Context, session string) (bool, error) {
	deadline := time.Now().Add(m.cleanup())
	for {
		sc, err := readServe(ctx, m.CLI)
		if err != nil {
			return false, err
		}
		if !sc.hasSession(session) {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(m.poll()):
		}
	}
}

func (m *Manager) readRecord() (record, bool, error) {
	if m.StateFile == "" {
		return record{}, false, nil
	}
	data, err := os.ReadFile(m.StateFile)
	if errors.Is(err, os.ErrNotExist) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, err
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil || rec.Version != 1 || rec.Session == "" {
		// A damaged record names nothing Atlas could safely act on.
		_ = os.Remove(m.StateFile)
		return record{}, false, nil
	}
	return rec, true, nil
}

func (m *Manager) writeRecord(rec record) error {
	if m.StateFile == "" {
		return nil
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.StateFile), 0o700); err != nil {
		return err
	}
	tmp := m.StateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.StateFile)
}

func (m *Manager) removeRecord() error {
	if m.StateFile == "" {
		return nil
	}
	if err := os.Remove(m.StateFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Link is a running Funnel entry owned by Atlas.
type Link struct {
	m        *Manager
	plan     Plan
	target   string
	session  string
	proc     Process
	progress *Progress
	closing  atomic.Bool
	once     sync.Once
	err      error
}

// URL is the public HTTPS address.
func (l *Link) URL() string { return l.plan.URL() }

// Plan returns the public host and port.
func (l *Link) Plan() Plan { return l.plan }

// Session is tailscaled's foreground session id for this entry.
func (l *Link) Session() string { return l.session }

// Target is the loopback URL Funnel forwards to.
func (l *Link) Target() string { return l.target }

// Progress returns the shared progress, including PhaseLost.
func (l *Link) Progress() *Progress { return l.progress }

// Alive reports whether the CLI that holds the entry still runs.
func (l *Link) Alive() bool {
	select {
	case <-l.proc.Done():
		return false
	default:
		return true
	}
}

func (l *Link) watch() {
	<-l.proc.Done()
	if l.closing.Load() {
		return
	}
	code, _ := l.proc.Result()
	l.progress.fail(PhaseLost, fmt.Sprintf("tailscale funnel exited (code %d): %s", code, l.progress.lastLines()))
}

// Close stops the CLI, waits until tailscaled no longer lists the
// session and removes the ownership record. It is idempotent.
func (l *Link) Close() error {
	l.once.Do(func() {
		l.closing.Store(true)
		l.proc.Stop(3 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), l.m.cleanup()+CommandTimeout)
		defer cancel()
		gone, err := l.m.waitGone(ctx, l.session)
		switch {
		case err != nil && errors.Is(err, ErrServiceUnavailable):
			// tailscaled is gone, and with it every foreground session.
			l.err = l.m.removeRecord()
		case err != nil:
			l.err = err
		case !gone:
			l.err = fmt.Errorf("%w (%s)", ErrResidual, l.plan.URL())
		default:
			l.err = l.m.removeRecord()
		}
		l.progress.Set(PhaseStopped)
	})
	return l.err
}
