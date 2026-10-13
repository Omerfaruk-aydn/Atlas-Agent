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
