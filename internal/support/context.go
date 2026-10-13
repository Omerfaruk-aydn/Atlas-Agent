	b.WriteString("The local browser, computer and bash tools act on THIS computer; never use them for the remote one. ")
	b.WriteString("Showing the user a remote screenshot you already have is fine.")
	if st.State == StateReconnecting {
		b.WriteString(" The connection is being restored: do not switch to this computer meanwhile.")
	}
	if st.State == StatePaused {
		b.WriteString(" The owner paused the session; wait for them.")
	}
	return b.String()
}

// ActiveRemote returns the supported computer's name and session id while
// a session is live (connected, paused or reconnecting), else empty
// strings.
func (c *Controller) ActiveRemote() (name, sessionID string) {
	if c == nil {
		return "", ""
	}
	st := c.Status()
	if st.Remote == nil {
		return "", ""
	}
	switch st.State {
	case StateConnected, StatePaused, StateReconnecting:
		return st.Remote.RemoteName, st.Remote.SessionID
	}
	return "", ""
}
