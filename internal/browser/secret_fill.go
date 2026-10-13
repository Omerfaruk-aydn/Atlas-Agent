	out.Selector, _ = m["selector"].(string)
	out.NodeToken, _ = m["node_token"].(string)
	out.Reason, _ = m["reason"].(string)
	out.Found = out.Count == 1 && out.Selector != ""
	if !out.Found && out.Reason == "" {
		out.Reason = map[bool]string{true: SecretTargetAmbiguous, false: SecretTargetMissing}[out.Count > 1]
	}
	return out, nil
}

// FillSecret types the secret into the bound field. Nothing is sent when
// the tab, document, frame, origin or field no longer match. The value is
// never read back; only its length is compared.
func (s *chromedpSession) FillSecret(parent context.Context, target SecretTarget, secret string) (SecretFillResult, error) {
	ctx, done := s.boundContext(parent, 10*time.Second)
	defer done()
	stale := func(reason string) (SecretFillResult, error) {
		return SecretFillResult{Status: SecretTargetStale, Reason: reason}, errors.New(SecretTargetStale + ": " + reason + "; nothing was sent")
	}
	if s.disconnected() || s.currentTabGone() {
		return stale("the browser tab is gone")
	}
	id := s.ExecutionIdentity(ctx)
	if target.TabID != "" && id.TabID != target.TabID {
		return stale("another tab is selected")
	}
	if target.Document != "" && id.Document != target.Document {
		return stale("the page navigated to a new document")
	}
	world, err := s.isolatedWorld(ctx, target.FrameID)
	if err != nil {
		return stale("the target frame is gone")
	}
	value, err := s.callIn(ctx, world, secretFocusScript, target.Origin, target.Selector, target.NodeToken)
	if err != nil {
		return SecretFillResult{Status: SecretNotSent, Reason: "target check failed"}, errors.New("target_missing: the password field could not be checked; nothing was sent")
	}
	if status, _ := value.(string); status != "focused" {
		switch status {
		case SecretTargetStale:
			return stale("the authorized password element was replaced")
		case SecretOriginMismatch:
			return SecretFillResult{Status: SecretOriginMismatch}, errors.New("scope_mismatch: the frame's origin differs from the authorized origin; nothing was sent")
		case SecretTargetAmbiguous:
			return SecretFillResult{Status: SecretTargetAmbiguous}, errors.New("ambiguous_target: more than one password field matches; nothing was sent")
		default:
			return SecretFillResult{Status: SecretTargetMissing, Reason: status}, errors.New("target_missing: " + status + "; nothing was sent")
		}
	}
	// The document may have changed while the field was focused.
	if again := s.ExecutionIdentity(ctx); again.TabID != id.TabID || again.Document != id.Document {
		return stale("the page changed during the fill")
	}
	inserted, err := s.callIn(ctx, world, secretInsertScript, target.Origin, target.NodeToken, secret)
	if err != nil {
		// The input event may or may not have reached the page.
		return SecretFillResult{Status: SecretFillUncertain, Dispatched: true, Reason: "input dispatch did not confirm"}, nil
	}
	if inserted != "sent" {
		return stale("the authorized password element was replaced")
	}
	ok, err := s.callIn(ctx, world, secretVerifyScript, len(utf16.Encode([]rune(secret))))
	if err != nil || ok != true {
		return SecretFillResult{Status: SecretFillUncertain, Dispatched: true, Reason: "the field did not confirm the expected length"}, nil
	}
	return SecretFillResult{Status: SecretFillDispatched, Dispatched: true}, nil
}

// LoginState observes the current top document's sign-in structure.
func (s *chromedpSession) LoginState(parent context.Context, expected string) (LoginState, error) {
	ctx, done := s.boundContext(parent, 5*time.Second)
	defer done()
	world, err := s.isolatedWorld(ctx, "")
	if err != nil {
		return LoginState{}, err
	}
	value, err := s.callIn(ctx, world, loginStateScript, expected)
	if err != nil {
		return LoginState{}, err
	}
	m, _ := value.(map[string]any)
	b := func(k string) bool { v, _ := m[k].(bool); return v }
	out := LoginState{
		PasswordVisible: b("password_visible"), PasswordInvalid: b("password_invalid"), PasswordFilled: b("password_filled"),
		AlertVisible: b("alert_visible"), OneTimeCode: b("one_time_code"), Captcha: b("captcha"), Passkey: b("passkey"),
		DeviceApproval: b("device_approval"), CredentialChange: b("credential_change"), ExpectedText: b("expected_text"),
		AuthenticatedUI: b("authenticated_ui"), PostLoginUI: b("post_login_ui"),
	}
	out.URL, _ = m["url"].(string)
	out.Origin, _ = m["origin"].(string)
	return out, nil
}
