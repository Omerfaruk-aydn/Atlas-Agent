	case e.req.State == CredentialExpired:
		fail = ErrCredentialExpired
	case e.req.State == CredentialCanceled:
		fail = ErrCredentialCanceled
	case e.req.State == CredentialConsumed:
		fail = ErrCredentialUsed
	case e.req.State == CredentialPending:
		fail = ErrCredentialNotReady
	}
	if fail != nil {
		c.mu.Unlock()
		c.emit(events)
		return CredentialRequest{}, fail
	}
	secret := string(e.secret)
	e.req.State = CredentialConsumed
	c.mu.Unlock()
	c.emit(events)

	dispatched, fillErr := fill(secret)
	c.mu.Lock()
	if !dispatched && fillErr != nil && e.req.State == CredentialConsumed && c.now().Before(e.req.ExpiresAt) {
		// Nothing reached the page; the user's authorization still stands.
		e.req.State = CredentialProvided
		view := e.req
		c.mu.Unlock()
		return view, fillErr
	}
	event := c.finishLocked(e, CredentialConsumed)
	view := e.req
	c.mu.Unlock()
	c.emit([]CredentialEvent{event})
	return view, fillErr
}

// Target returns the bound target of an unconsumed request of a session.
func (c *Credentials) Target(id, sessionID string) (CredentialTarget, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok || e.req.SessionID != sessionID {
		return CredentialTarget{}, false
	}
	return e.req.Target, true
}

// PurgeSession ends every open credential of a session, for task
// cancellation or session deletion.
func (c *Credentials) PurgeSession(sessionID string) int {
	c.purgeChat(sessionID)
	return c.purge(func(r CredentialRequest) bool { return r.SessionID == sessionID })
}

// PurgeDevice ends every open credential supplied by a revoked device.
func (c *Credentials) PurgeDevice(deviceID string) int {
	if deviceID == "" {
		return 0
	}
	return c.purge(func(r CredentialRequest) bool { return r.ProvidedBy == deviceID })
}

// PurgeAll ends every open credential, for shutdown.
func (c *Credentials) PurgeAll() int {
	c.purgeChat("")
	return c.purge(func(CredentialRequest) bool { return true })
}

func (c *Credentials) purge(match func(CredentialRequest) bool) int {
	c.mu.Lock()
	var events []CredentialEvent
	for _, e := range c.entries {
		if (e.req.State == CredentialPending || e.req.State == CredentialProvided) && match(e.req) {
			events = append(events, c.finishLocked(e, CredentialCanceled))
		}
	}
	c.mu.Unlock()
	c.emit(events)
	return len(events)
}

// sweepLocked expires stale requests and forgets long-finished ones.
func (c *Credentials) sweepLocked() []CredentialEvent {
	now := c.now()
	var events []CredentialEvent
	for id, e := range c.entries {
		switch e.req.State {
		case CredentialPending, CredentialProvided:
			if !now.Before(e.req.ExpiresAt) {
				events = append(events, c.finishLocked(e, CredentialExpired))
			}
		default:
			if now.Sub(e.req.ExpiresAt) > 10*time.Minute {
				delete(c.entries, id)
			}
		}
	}
	return events
}

// finishLocked ends a lifetime: the secret buffer is cleared and dropped.
func (c *Credentials) finishLocked(e *credentialEntry, state CredentialState) CredentialEvent {
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	clear(e.secret)
	e.secret = nil
	if state != CredentialExpired {
		// Keep finished views briefly so late requests get a clear reason.
		e.req.ExpiresAt = c.now()
	}
	return c.transitionLocked(e, state)
}

// scheduleExpiryLocked releases credentials even when no new request arrives.
func (c *Credentials) scheduleExpiryLocked(e *credentialEntry, delay time.Duration) {
	if e.timer != nil {
		e.timer.Stop()
	}
	deadline := e.req.ExpiresAt
	e.timer = time.AfterFunc(delay, func() {
		c.mu.Lock()
		if e.req.ExpiresAt != deadline || (e.req.State != CredentialPending && e.req.State != CredentialProvided) {
			c.mu.Unlock()
			return
		}
		event := c.finishLocked(e, CredentialExpired)
		c.mu.Unlock()
		c.emit([]CredentialEvent{event})
	})
}

func (c *Credentials) transitionLocked(e *credentialEntry, state CredentialState) CredentialEvent {
	e.req.State = state
	close(e.changed)
	e.changed = make(chan struct{})
	return CredentialEvent{Request: e.req}
}

func (c *Credentials) emit(events []CredentialEvent) {
	if len(events) == 0 {
		return
	}
	c.mu.Lock()
	subs := make([]func(CredentialEvent), 0, len(c.channels))
	for _, fn := range c.channels {
		subs = append(subs, fn)
	}
	c.mu.Unlock()
	for _, ev := range events {
		for _, fn := range subs {
			fn(ev)
		}
	}
}

func randomHandle() string {
	b := make([]byte, 15)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}
