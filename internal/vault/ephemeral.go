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
