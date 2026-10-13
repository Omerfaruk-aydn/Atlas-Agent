		return true
	}
	return false
}

// ChatHasCredential is a non-secret hint for password-field recovery.
func (c *Credentials) ChatHasCredential(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.chat[sessionID]
	return e != nil && c.now().Before(e.expires)
}

func chatLoginOrigin(prompt string) string {
	lower := strings.ToLower(prompt)
	// A mail domain is not its identity provider. These provider names map
	// to their actual login origin rather than the mailbox domain.
	known := map[string]string{"gmail": "https://accounts.google.com", "google": "https://accounts.google.com", "outlook": "https://login.microsoftonline.com", "microsoft": "https://login.microsoftonline.com"}
	origin := ""
	for name, candidate := range known {
		if !strings.Contains(lower, name) {
			continue
		}
		if origin != "" && origin != candidate {
			return ""
		}
		origin = candidate
	}
	if origin != "" {
		return origin
	}
	for _, raw := range chatURL.FindAllString(prompt, -1) {
		u, err := url.Parse(raw)
		if err != nil || u.User != nil {
			return ""
		}
		candidate, err := CredentialOrigin(u.Scheme + "://" + u.Host)
		if err != nil || (origin != "" && origin != candidate) {
			return ""
		}
		origin = candidate
	}
	return origin
}

// bindChatLocked creates a single-use handle only for the explicitly named
// account and origin, bound to the driver-observed live field.
func (c *Credentials) bindChatLocked(spec CredentialSpec, origin, account, service string) *credentialEntry {
	e := c.chat[spec.SessionID]
	if e == nil {
		return nil
	}
	if !c.now().Before(e.expires) {
		c.clearChatLocked(e)
		return nil
	}
	if origin != e.origin || account == "" || !strings.EqualFold(account, e.account) || spec.Target.TabID == "" || spec.Target.Document == "" || spec.Target.Selector == "" || len(c.entries) >= maxCredentialsTotal {
		return nil
	}
	now := c.now()
	entry := &credentialEntry{changed: make(chan struct{}), secret: append([]byte(nil), e.secret...), req: CredentialRequest{
		ID: credentialHandleBits + randomHandle(), SessionID: spec.SessionID, InteractionID: spec.InteractionID,
		Service: service, Origin: origin, Account: account, State: CredentialProvided, ProvidedBy: "user_prompt",
		CreatedAt: now, ExpiresAt: now.Add(CredentialSecretTTL), Target: spec.Target,
	}}
	c.entries[entry.req.ID] = entry
	c.scheduleExpiryLocked(entry, CredentialSecretTTL)
	c.clearChatLocked(e)
	return entry
}

func (c *Credentials) clearChatLocked(e *chatCredential) {
	if e.timer != nil {
		e.timer.Stop()
	}
	for i := range e.secret {
		e.secret[i] = 0
	}
	e.secret = nil
	delete(c.chat, e.session)
}

func (c *Credentials) purgeChat(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, e := range c.chat {
		if sessionID == "" || id == sessionID {
			c.clearChatLocked(e)
		}
	}
	if sessionID == "" {
		clear(c.sources)
	} else {
		delete(c.sources, sessionID)
	}
}
