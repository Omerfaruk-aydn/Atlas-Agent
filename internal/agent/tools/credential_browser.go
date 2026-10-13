func credentialAuthorized(action string, params BrowserParams, sessionID string) bool {
	switch action {
	case "credential_request":
		return true
	case "vault_fill":
		if !vault.IsCredentialHandle(params.CredentialID) {
			return false
		}
		req, ok := vault.Ephemeral.Get(params.CredentialID)
		return ok && req.SessionID == sessionID && req.State == vault.CredentialProvided
	}
	return false
}

// credentialOwnedOutcome keeps the credential actions' own outcome claims
// (fill dispatched is not login verified) instead of the generic one.
func credentialOwnedOutcome(action string, params BrowserParams) bool {
	return action == "login_check" || action == "vault_fill" && vault.IsCredentialHandle(params.CredentialID)
}

// refusedSecretCandidates are the values a refused password-field call
// tried to send.
func refusedSecretCandidates(params BrowserParams) []string {
	var out []string
	for _, v := range []string{params.Advanced.Value, params.Text, params.Advanced.Text} {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	for _, f := range params.Advanced.Fields {
		if strings.TrimSpace(f.Value) != "" {
			out = append(out, f.Value)
		}
	}
	return out
}

// browserSessionFor returns the chat's tab without bootstrapping a
// navigation.
func browserSessionFor(ctx context.Context, sessions browserSessions, sessionID string) (browser.Session, error) {
	if manager, ok := sessions.(interface {
		SessionContext(context.Context, string) (browser.Session, error)
	}); ok {
		return manager.SessionContext(ctx, sessionID)
	}
	return sessions.Session(sessionID)
}
