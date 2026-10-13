	// No --bg: the entry lives only as long as Atlas' process. No --yes:
	// an unexpected prompt fails instead of being confirmed silently.
	proc, err := m.CLI.Start([]string{"funnel", "--https=" + strconv.Itoa(int(plan.Port)), target}, progress.line)
	if err != nil {
		return nil, err
	}
	stop := func() { proc.Stop(3 * time.Second) }
	started := time.Now()
	deadline := started.Add(m.confirm())
	extended := false
	tick := time.NewTicker(m.poll())
	defer tick.Stop()
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			stop()
			return nil, ctx.Err()
		case <-proc.Done():
			if u := progress.approvalURL(); u != "" {
				return nil, &ApprovalRequiredError{URL: u}
			}
			code, _ := proc.Result()
			detail := progress.lastLines()
			if strings.Contains(detail, "listener already exists") || strings.Contains(detail, "already exists for port") {
				return nil, &PortConflictError{Occupants: []Occupant{{Port: plan.Port, What: detail}}}
			}
			return nil, fmt.Errorf("tailscale funnel exited (code %d): %s", code, detail)
		case <-tick.C:
		}
		if !extended && progress.approvalURL() != "" {
			extended = true
			progress.Set(PhaseApproval)
			deadline = started.Add(m.approve())
		}
		sc, err := readServe(ctx, m.CLI)
		if err == nil {
			if id, ok := sc.owned(plan.Host, plan.Port, target); ok {
				l := &Link{m: m, plan: plan, target: target, session: id, proc: proc, progress: progress}
				rec := record{Version: 1, Process: proc.Identity(), Session: id, Host: plan.Host, Port: plan.Port, Target: target, StartedAt: time.Now().UTC()}
				if err := m.writeRecord(rec); err != nil {
					stop()
					return nil, fmt.Errorf("record the Funnel owner: %w", err)
				}
				progress.Set(PhaseReady)
				go l.watch()
				return l, nil
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			stop()
			if lastErr != nil {
				return nil, fmt.Errorf("%w: Funnel entry did not appear: %v", ErrTimeout, lastErr)
			}
			return nil, fmt.Errorf("%w: Funnel entry did not appear", ErrTimeout)
		}
	}
}

// Recover handles a run that ended without Close, for example a crash.
// It stops only the CLI process Atlas recorded (matched by PID and
// creation time) and waits for its session to disappear. It never
// removes or edits any Serve or Funnel entry itself.
func (m *Manager) Recover(ctx context.Context) error {
	rec, ok, err := m.readRecord()
	if err != nil || !ok {
		return err
	}
	if procown.Matches(rec.Process) {
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
