	chatEmail    = regexp.MustCompile(`[A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	chatURL      = regexp.MustCompile(`https://[^\s<>"']+`)
)

// CaptureUserPrompt separates an explicitly supplied login password from
// the instruction before persistence or provider dispatch. Ambiguous input
// is not authorized for filling. Quoted passwords may contain spaces.
func (c *Credentials) CaptureUserPrompt(sessionID, prompt string) (string, error) {
	matches := chatPassword.FindAllStringSubmatchIndex(prompt, -1)
	if len(matches) != 1 || sessionID == "" || strings.Contains(prompt, "```") {
		return prompt, nil
	}
	m := matches[0]
	instruction := prompt[:m[0]] + prompt[m[1]:]
	accounts := chatEmail.FindAllString(instruction, -1)
	if len(accounts) == 0 {
		return prompt, nil
	}
	account := accounts[0]
	for _, other := range accounts[1:] {
		if !strings.EqualFold(account, other) {
			return prompt, nil
		}
	}
	origin := chatLoginOrigin(instruction)
	if origin == "" {
		return prompt, nil
	}
	secret := prompt[m[2]:m[3]]
	quoted := len(secret) >= 2 && (secret[0] == '"' || secret[0] == '\'')
	if quoted {
		secret = secret[1 : len(secret)-1]
	}
	if !quoted && credentialDescription(secret) {
		return prompt, nil
	}
	if len(secret) == 0 || len(secret) > maxCredentialSecret {
		return "", errors.New("invalid explicitly supplied credential")
	}
	// Register before returning the sanitized prompt so later echoes are
	// redacted without retaining a second plaintext copy.
	if err := registerTemporary(sessionID, secret); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.chat) >= maxCredentialsTotal && c.chat[sessionID] == nil {
		return "", ErrCredentialBudget
	}
	if old := c.chat[sessionID]; old != nil {
		c.clearChatLocked(old)
	}
	e := &chatCredential{session: sessionID, origin: origin, account: account, secret: []byte(secret), expires: c.now().Add(CredentialRequestTTL)}
	c.chat[sessionID] = e
	e.timer = time.AfterFunc(CredentialRequestTTL, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.chat[sessionID] == e {
			c.clearChatLocked(e)
		}
	})
	return Redact(sessionID, prompt[:m[0]]+" [credential supplied]"+prompt[m[1]:]) +
		"\n[Atlas credential: the user already supplied a one-use password for " + account + " at " + origin +
		". At the visible password field call browser action credential_request with that account, then browser action vault_fill with the returned handle. These are actions of the browser tool, not separate tools. Do not ask for the password again or hand off. Never put the value in tool arguments.]", nil
}

// Descriptions of a missing or failing password are not supplied values.
func credentialDescription(value string) bool {
	switch strings.ToLower(strings.Trim(value, ".,;!?")) {
	case "yok", "yanlış", "yanlis", "hatalı", "hatali", "unuttum", "bilmiyorum", "nedir", "ne", "lazım", "lazim", "gerekli", "girin", "gir", "girmem", "girmemiz", "değişti", "degisti", "eksik", "missing", "unknown", "incorrect", "wrong", "forgotten":
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
