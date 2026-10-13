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
