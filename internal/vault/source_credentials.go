
// SupplyFromSource stages a credential read by a trusted tool after that
// tool checked the human's source-to-destination instruction. sourceKey is
// non-secret provenance for the human turn and selected source. Re-reading
// the same source never re-arms a consumed or uncertain fill.
func (c *Credentials) SupplyFromSource(sessionID, sourceKey, origin, account string, secret []byte) (bool, error) {
	origin, err := CredentialOrigin(origin)
	if err != nil {
		return false, err
	}
	account = strings.TrimSpace(account)
	if sessionID == "" || sourceKey == "" || len(sourceKey) > 256 || account == "" || len(account) > 320 || len(secret) == 0 || len(secret) > maxCredentialSecret {
		return false, ErrCredentialScope
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for id, seen := range c.sources {
		for key, expiry := range seen {
			if !now.Before(expiry) {
				delete(seen, key)
			}
		}
		if len(seen) == 0 {
			delete(c.sources, id)
		}
	}
	seen := c.sources[sessionID]
	if _, ok := seen[sourceKey]; ok {
		e := c.chat[sessionID]
		return e != nil && e.origin == origin && strings.EqualFold(e.account, account) && now.Before(e.expires), nil
	}
	if len(c.sources) >= maxCredentialsTotal && seen == nil || len(seen) >= maxPendingPerSession || len(c.chat) >= maxCredentialsTotal && c.chat[sessionID] == nil {
		return false, ErrCredentialBudget
	}
	if err := registerTemporary(sessionID, string(secret)); err != nil {
		return false, err
	}
	if old := c.chat[sessionID]; old != nil {
		c.clearChatLocked(old)
	}
	e := &chatCredential{session: sessionID, origin: origin, account: account, secret: append([]byte(nil), secret...), expires: now.Add(CredentialRequestTTL)}
	c.chat[sessionID] = e
	if seen == nil {
		seen = make(map[string]time.Time)
		c.sources[sessionID] = seen
	}
	seen[sourceKey] = now.Add(CredentialRequestTTL + CredentialSecretTTL)
	e.timer = time.AfterFunc(CredentialRequestTTL, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.chat[sessionID] == e {
			c.clearChatLocked(e)
		}
	})
	return true, nil
}
