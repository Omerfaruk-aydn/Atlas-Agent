device_approval:has("check your phone","tap yes on","approve sign-in","telefonunuzu kontrol edin","telefonunu kontrol et","oturum açmayı onayla"),
credential_change:[...document.querySelectorAll('input[autocomplete="new-password"]')].some(vis),
authenticated_ui:signedIn,
post_login_ui:continueBtn||postLoginText,
expected_text:!!expected&&(text+'\n'+document.title.toLowerCase()).includes(expected.toLowerCase())}}`

func (s *chromedpSession) boundContext(parent context.Context, d time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(s.currentContext(), d)
	stop := context.AfterFunc(parent, cancel)
	return ctx, func() { stop(); cancel() }
}

// isolatedWorld returns an execution context in frameID (the top frame when
// empty) that page scripts cannot observe.
func (s *chromedpSession) isolatedWorld(ctx context.Context, frameID string) (float64, error) {
	if frameID == "" {
		tree, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false)
		if err != nil {
			return 0, err
		}
		frame := browserFrameInfo(tree, "")
		if frame == nil {
			return 0, errors.New("target_missing: no top frame")
		}
		frameID, _ = frame["id"].(string)
	}
	v, err := s.command(ctx, "Page.createIsolatedWorld", map[string]any{"frameId": frameID, "worldName": "atlas-secret"}, false)
	if err != nil {
		return 0, err
	}
	world, _ := v["executionContextId"].(float64)
	if world == 0 {
		return 0, errors.New("target_missing: frame has no execution context")
	}
	return world, nil
}

func (s *chromedpSession) callIn(ctx context.Context, world float64, fn string, args ...any) (any, error) {
	arguments := make([]map[string]any, 0, len(args))
	for _, a := range args {
		arguments = append(arguments, map[string]any{"value": a})
	}
	v, err := s.command(ctx, "Runtime.callFunctionOn", map[string]any{"functionDeclaration": fn, "executionContextId": world, "arguments": arguments, "returnByValue": true, "awaitPromise": true}, false)
	if err != nil {
		return nil, err
	}
	if v["exceptionDetails"] != nil {
		return nil, errors.New("target_missing: page evaluation failed")
	}
	r, _ := v["result"].(map[string]any)
	return r["value"], nil
}

// PasswordTarget finds the single visible password field for a credential
// card and records the tab, document and frame it lives in.
func (s *chromedpSession) PasswordTarget(parent context.Context, frameID, selector string) (PasswordTarget, error) {
	ctx, done := s.boundContext(parent, 5*time.Second)
	defer done()
	id := s.ExecutionIdentity(ctx)
	world, err := s.isolatedWorld(ctx, frameID)
	if err != nil {
		return PasswordTarget{}, err
	}
	value, err := s.callIn(ctx, world, passwordTargetScript, selector)
	if err != nil {
		return PasswordTarget{}, err
	}
	m, _ := value.(map[string]any)
	count, _ := m["count"].(float64)
	out := PasswordTarget{Count: int(count), TabID: id.TabID, Document: id.Document, FrameID: frameID}
	out.Origin, _ = m["origin"].(string)
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
