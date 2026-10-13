// selectors. Verified requires the caller's own expectation to match.
func classifyLogin(s browser.LoginState, expectedURL string, expectText bool) (login, verify, next string) {
	switch {
	case s.Captcha:
		return "user_verification_required", "captcha", NextCaptchaSolve
	case s.Passkey:
		return "user_verification_required", "passkey", NextUserVerification
	case s.DeviceApproval:
		return "user_verification_required", "device_approval", NextUserVerification
	case s.OneTimeCode:
		return "user_verification_required", "one_time_code", NextUserVerification
	case s.CredentialChange:
		return "user_verification_required", "credential_change", NextUserVerification
	case s.PasswordVisible && (s.PasswordInvalid || s.AlertVisible):
		return "login_failed", "", NextCredentialRequired
	case s.PasswordVisible:
		return "login_uncertain", "password_step_still_visible", ""
	}
	// A post-login screen (Devam Et, Hoş geldiniz, dashboard, address
	// verification) with no password field visible means the login landed.
	if s.PostLoginUI && !s.PasswordVisible {
		return "login_verified", "", ""
	}
	if expectedURL == "" && !expectText {
		return "login_uncertain", "no_expectation_given", ""
	}
	if expectedURL != "" && loginURLMatches(s.URL, expectedURL) && expectText && s.ExpectedText && s.AuthenticatedUI {
		return "login_verified", "", ""
	}
	if s.PostLoginUI {
		return "login_verified", "", ""
	}
	return "login_uncertain", "expectation_not_matched", ""
}

// Login proof requires both a structurally matched URL and visible evidence.
func loginURLMatches(actual, expected string) bool {
	a, err := url.Parse(actual)
	if err != nil {
		return false
	}
	e, err := url.Parse(expected)
	if err != nil || a.User != nil || e.User != nil || e.Host == "" || e.RawQuery != "" || e.Fragment != "" {
		return false
	}
	if !strings.EqualFold(a.Scheme, e.Scheme) || !strings.EqualFold(a.Host, e.Host) {
		return false
	}
	path := strings.TrimSuffix(e.EscapedPath(), "/")
	return a.EscapedPath() == path || strings.HasPrefix(a.EscapedPath(), path+"/")
}

// persistCredential saves a used temporary credential when the user asked
// for it. It reports the outcome without the value.
func persistCredential(ctx context.Context, store *vault.Store, req vault.CredentialRequest, secret string) string {
	if store == nil || store.State == nil {
		return "persist_failed: vault unavailable"
	}
	if req.Account == "" {
		return "persist_failed: no account name to save under"
	}
	id := "saved_" + strings.TrimPrefix(req.ID, "cr_")
	if err := store.Add(ctx, id, req.Origin, req.Account, secret); err != nil {
		return "persist_failed: " + err.Error()
	}
	return "saved_to_vault:" + id
}

// credentialAuthorized reports that the user's own phone action already
// authorized this exact fill: a provided temporary handle of this task.
// credential_request itself only opens a card and changes nothing.
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
