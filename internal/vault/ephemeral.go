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
