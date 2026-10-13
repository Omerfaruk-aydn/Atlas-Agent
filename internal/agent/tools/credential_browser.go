		meta.State, meta.NextStep = string(vault.CredentialExpired), NextHandleExpired
		return waitingForUser("The credential card on the phone was not answered in time and is closed. Nothing was filled.", meta), nil
	}
	meta.State = string(final.State)
	switch final.State {
	case vault.CredentialProvided:
		meta.Status, meta.NextStep = "credential_ready", NextCredentialFill
		return withMergedMetadata(withToolOutcome(fantasy.NewTextResponse(fmt.Sprintf("The user supplied a temporary credential for %s on their phone. Handle: %s (single use, expires in about %d minutes; the value is never shown to you). Next: vault_fill with credential_id %s and the same password field, then submit and call login_check.", final.Origin, final.ID, int(vault.CredentialSecretTTL.Minutes()), final.ID)), ToolOutcome{Dispatch: "none", Claim: "credential_ready"}), meta), nil
	case vault.CredentialExpired:
		meta.NextStep = NextHandleExpired
		return waitingForUser("The credential card expired before the user answered. Nothing was filled.", meta), nil
	default:
		meta.NextStep = NextCredentialRequired
		return waitingForUser("The user canceled the credential card (or the phone disconnected). Nothing was filled; do not ask for the password in chat.", meta), nil
	}
}

// fillBrowserCredential places a temporary credential into its bound
// field. The handle is consumed before dispatch, so it can never be sent
// twice; a refusal before dispatch keeps it usable.
func fillBrowserCredential(ctx context.Context, sess browser.Session, store *vault.Store, params BrowserParams, sessionID string) (fantasy.ToolResponse, error) {
	filler, ok := sess.(browser.SecretFiller)
	meta := credentialMeta{Credential: params.CredentialID}
	if !ok {
		return credentialError("This browser driver cannot fill a temporary credential.", meta), nil
	}
	origin, err := pageOrigin(sess)
	if err != nil {
		meta.NextStep = NextScopeMismatch
		return credentialError("The current page has no credential origin; nothing was sent.", meta), nil
	}
	meta.Origin = origin
	target, ok := vault.Ephemeral.Target(params.CredentialID, sessionID)
	if !ok {
		meta.NextStep = NextHandleExpired
		return credentialError("Unknown or ended credential handle for this task; nothing was sent. Request a new credential card.", meta), nil
	}
	selector := target.Selector
	if params.Ref != "" || params.Selector != "" {
		var err error
		selector, err = resolveTargetSelector("vault_fill", params)
		if err != nil {
			return credentialError(err.Error(), meta), nil
		}
	}
	probe, err := filler.PasswordTarget(ctx, target.FrameID, selector)
	if err != nil || !probe.Found || probe.Document != target.Document || probe.TabID != target.TabID || probe.FrameID != target.FrameID || probe.NodeToken != target.NodeToken {
		meta.NextStep = NextTargetStale
		return credentialError("target_stale: the given field is not the password field the user authorized; nothing was sent. Request a new credential card for the current field.", meta), nil
	}
	// An authorized iframe may have a different origin from its parent page.
	// The current pinned password field, not the parent, establishes scope.
	frameOrigin, err := vault.CredentialOrigin(probe.Origin)
	if err != nil {
		meta.NextStep = NextScopeMismatch
		return credentialError("The password frame has no valid credential origin; nothing was sent.", meta), nil
	}
	if target.FrameID == "" && frameOrigin != origin {
		meta.NextStep = NextScopeMismatch
		return credentialError("The password field origin does not match the current page; nothing was sent.", meta), nil
	}
	origin = frameOrigin
	meta.Origin = origin
	var fill browser.SecretFillResult
	view, _ := vault.Ephemeral.Get(params.CredentialID)
	saved := ""
	req, err := vault.Ephemeral.Use(params.CredentialID, sessionID, origin, strings.TrimSpace(params.Account), func(secret string) (bool, error) {
		var fillErr error
		fill, fillErr = filler.FillSecret(ctx, browser.SecretTarget{Origin: origin, TabID: target.TabID, Document: target.Document, FrameID: target.FrameID, Selector: target.Selector, NodeToken: target.NodeToken}, secret)
		if fill.Dispatched && view.Persist {
			// Saving is the user's explicit choice on the phone and the
			// only path that writes this value to the protected vault.
			saved = persistCredential(ctx, store, view, secret)
		}
		return fill.Dispatched, fillErr
	})
	meta.State, meta.Fill = string(req.State), fill.Status
	switch {
	case errors.Is(err, vault.ErrCredentialScope):
		meta.NextStep = NextScopeMismatch
		return credentialError("scope_mismatch: this handle is bound to another task, origin or account; nothing was sent.", meta), nil
	case errors.Is(err, vault.ErrCredentialExpired), errors.Is(err, vault.ErrCredentialUnknown), errors.Is(err, vault.ErrCredentialCanceled):
		meta.NextStep = NextHandleExpired
		return credentialError(err.Error()+"; nothing was sent.", meta), nil
	case errors.Is(err, vault.ErrCredentialUsed):
		meta.NextStep = NextLoginCheck
		return credentialError("credential_used: this handle was already filled once and is never sent twice. Observe the page with login_check; if the password was wrong, request a new card.", meta), nil
	case errors.Is(err, vault.ErrCredentialNotReady):
		meta.NextStep = NextCredentialRequired
		return credentialError("The user has not supplied this credential yet; nothing was sent.", meta), nil
	case !fill.Dispatched:
		meta.NextStep = NextTargetStale
		msg := "nothing was sent"
		if err != nil {
			msg = err.Error()
		}
		return credentialError(msg+". The handle is valid only for its original target. Observe the password field; a changed target needs a new credential card.", meta), nil
	}
	if view.Persist {
		meta.Verify = saved
	}
	meta.NextStep = NextLoginCheck
	if fill.Status == browser.SecretFillUncertain {
		meta.Status = "fill_uncertain"
		resp := withToolOutcome(fantasy.NewTextResponse("fill_uncertain: input was dispatched to the authorized password field but the field did not confirm it. The handle is used up and is not resent. Observe with login_check before anything else."), ToolOutcome{Dispatch: "unknown", Mutation: true, Claim: "fill_uncertain"})
		return withMergedMetadata(resp, meta), nil
	}
	meta.Status = "fill_dispatched"
	resp := withToolOutcome(fantasy.NewTextResponse("fill_dispatched: the temporary password was typed into the authorized field (value not read back). This does not prove sign-in. Submit the form, then call login_check."), ToolOutcome{Dispatch: "sent", Mutation: true, Claim: "fill_dispatched"})
	return withMergedMetadata(resp, meta), nil
}

// runLoginCheck classifies the sign-in state from structure, keeping
// "filled", "failed", "waiting for the user" and "verified" apart.
func runLoginCheck(ctx context.Context, sess browser.Session, params BrowserParams) (fantasy.ToolResponse, error) {
	prober, ok := sess.(browser.LoginProber)
	if !ok {
		return fantasy.NewTextErrorResponse("This browser driver cannot observe sign-in state."), nil
	}
	expected := strings.TrimSpace(params.Advanced.Expected)
	state, err := prober.LoginState(ctx, expected)
	if err != nil {
		return fantasy.NewTextErrorResponse("login_check could not observe the page: " + err.Error()), nil
	}
	login, verify, next := classifyLogin(state, strings.TrimSpace(params.URL), expected != "")
	meta := credentialMeta{Status: login, Login: login, Verify: verify, NextStep: next, Origin: state.Origin}
	data, _ := json.Marshal(struct {
		Login        string `json:"login_status"`
		Verification string `json:"verification,omitempty"`
		NextStep     string `json:"next_step,omitempty"`
		URL          string `json:"url"`
		browser.LoginState
	}{login, verify, next, state.URL, state})
	resp := withToolOutcome(fantasy.NewTextResponse(string(data)), ToolOutcome{Dispatch: "none", Verified: login == "login_verified", Source: "observation", Claim: login})
	if verify == "captcha" {
		resp.Content += "\n\nA CAPTCHA is blocking the login. Call browser action captcha_solve now: it returns the challenge image; read the characters and call captcha_fill with the answer. Then submit and call login_check again. If the CAPTCHA answer was wrong (login_check still reports captcha), call captcha_solve again for a fresh image and retry with the new answer. Do not hand off and do not stop."
	}
	return withMergedMetadata(resp, meta), nil
}

// classifyLogin is generic: it reads page structure, not one provider's
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
