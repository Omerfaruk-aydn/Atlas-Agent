	})
	defer unsub()
	resp := runCredTool(t, t.Context(), sess, &mockPermissionService{}, session, BrowserParams{Action: "credential_request"})
	handle := metaOf(t, resp)["credential_id"].(string)

	other := runCredTool(t, t.Context(), sess, &mockPermissionService{}, "cred-intruder", BrowserParams{Action: "vault_fill", CredentialID: handle})
	require.True(t, other.IsError)
	require.Empty(t, sess.fills)

	sess.url = "https://evil.example.test/login"
	wrong := runCredTool(t, t.Context(), sess, &mockPermissionService{}, session, BrowserParams{Action: "vault_fill", CredentialID: handle})
	require.True(t, wrong.IsError)
	require.Equal(t, NextScopeMismatch, metaOf(t, wrong)["next_step"])
	require.Empty(t, sess.fills)
}

func TestTemporaryCredentialUncertainFillIsNotResent(t *testing.T) {
	session := "cred-uncertain"
	sess := newSecretPage()
	sess.fillResult = browser.SecretFillResult{Status: browser.SecretFillUncertain, Dispatched: true}
	unsub := phone(t, session, func(r vault.CredentialRequest) {
		_, _ = vault.Ephemeral.Provide(r.ID, session, "phone-test", []byte(credSecret), false)
	})
	defer unsub()
	resp := runCredTool(t, t.Context(), sess, &mockPermissionService{}, session, BrowserParams{Action: "credential_request"})
	handle := metaOf(t, resp)["credential_id"].(string)
	fill := runCredTool(t, t.Context(), sess, &mockPermissionService{}, session, BrowserParams{Action: "vault_fill", CredentialID: handle})
	require.Equal(t, "fill_uncertain", metaOf(t, fill)["status"])
	again := runCredTool(t, t.Context(), sess, &mockPermissionService{}, session, BrowserParams{Action: "vault_fill", CredentialID: handle})
	require.True(t, again.IsError)
	require.Len(t, sess.fills, 1)
}

func TestCredentialCardCanceledByUserStopsTurn(t *testing.T) {
	session := "cred-cancel"
	sess := newSecretPage()
	unsub := phone(t, session, func(r vault.CredentialRequest) { _, _ = vault.Ephemeral.Cancel(r.ID, session) })
	defer unsub()
	resp := runCredTool(t, t.Context(), sess, &mockPermissionService{}, session, BrowserParams{Action: "credential_request"})
	require.True(t, resp.StopTurn)
	meta := metaOf(t, resp)
	require.Equal(t, "waiting_for_user", meta["status"])
	outcome, _ := meta["atlas_outcome"].(map[string]any)
	require.Equal(t, "waiting_for_user", outcome["claim"], "waiting is never success")
	require.Empty(t, sess.fills)
}

func TestCredentialWaitEndsWhenTaskIsCanceled(t *testing.T) {
	session := "cred-task-cancel"
	sess := newSecretPage()
	unsub := vault.Ephemeral.Subscribe(func(vault.CredentialEvent) {})
	defer unsub()
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		for range 100 {
			if len(vault.Ephemeral.Pending(session)) > 0 {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	input, _ := json.Marshal(BrowserParams{Action: "credential_request"})
	_, err := newBrowserTool(&mockPermissionService{}, t.TempDir(), &secretSessions{sess: sess}, browserDescription(false)).Run(context.WithValue(ctx, SessionIDContextKey, session), fantasy.ToolCall{ID: "c1", Name: BrowserToolName, Input: string(input)})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, vault.Ephemeral.Pending(session), "a canceled task leaves no open card")
}

// Plan mode is read-only: it neither opens a card nor types a credential
// the user already provided.
func TestPlanModeBlocksCredentialFill(t *testing.T) {
	session := "cred-plan"
	sess := newSecretPage()
	unsub := phone(t, session, func(r vault.CredentialRequest) {
		_, _ = vault.Ephemeral.Provide(r.ID, session, "phone-test", []byte(credSecret), false)
	})
	defer unsub()
	plan := &modePermissions{mode: permission.ModePlan}
	resp := runCredTool(t, t.Context(), sess, plan, session, BrowserParams{Action: "credential_request", Service: "Example"})
	require.True(t, resp.IsError)
	require.Empty(t, vault.Ephemeral.Pending(session), "no card opens in plan mode")

	resp = runCredTool(t, t.Context(), sess, &modePermissions{mode: permission.ModeManual}, session, BrowserParams{Action: "credential_request", Service: "Example"})
	require.False(t, resp.IsError, resp.Content)
	handle := metaOf(t, resp)["credential_id"].(string)
	fill := runCredTool(t, t.Context(), sess, plan, session, BrowserParams{Action: "vault_fill", CredentialID: handle})
	require.True(t, fill.IsError)
	require.Empty(t, sess.fills)
	view, ok := vault.Ephemeral.Get(handle)
	require.True(t, ok)
	require.Equal(t, vault.CredentialProvided, view.State, "a refused fill does not consume the value")
}

func TestPasswordFieldRefusalGivesNextStepAndProtectsValue(t *testing.T) {
	session := "cred-refusal"
	sess := newSecretPage()
	sess.typeErr = errors.New("password_field: use vault_fill for saved credentials; passwords never go through field actions")
	unsub := vault.Ephemeral.Subscribe(func(vault.CredentialEvent) {})
	defer unsub()
	value := "Synth-Chat-Pass-551"
	resp := runCredTool(t, t.Context(), sess, &mockPermissionService{}, session, BrowserParams{Action: "type", Selector: "#pw", Text: value})
	require.True(t, resp.IsError)
	meta := metaOf(t, resp)
	require.Equal(t, "password_field", meta["error_code"])
	require.Equal(t, NextCredentialRequired, meta["next_step"])
	require.Contains(t, resp.Content, "credential_request")
	require.True(t, vault.Sensitive(session))
	require.NotContains(t, vault.Redact(session, `{"text":"`+value+`"}`), value)
}

func TestLoginCheckSeparatesOutcomes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state       browser.LoginState
		url, expect string
		login, why  string
	}{
		{browser.LoginState{PasswordVisible: true, AlertVisible: true}, "", "", "login_failed", ""},
		{browser.LoginState{OneTimeCode: true}, "", "", "user_verification_required", "one_time_code"},
		{browser.LoginState{Passkey: true}, "", "", "user_verification_required", "passkey"},
		{browser.LoginState{Captcha: true, PasswordVisible: true}, "", "", "user_verification_required", "captcha"},
		{browser.LoginState{DeviceApproval: true}, "", "", "user_verification_required", "device_approval"},
		{browser.LoginState{PasswordVisible: true}, "", "", "login_uncertain", "password_step_still_visible"},
		{browser.LoginState{URL: "https://mail.example.test/inbox"}, "", "", "login_uncertain", "no_expectation_given"},
		{browser.LoginState{URL: "https://mail.example.test/inbox"}, "https://mail.example.test/", "", "login_uncertain", "expectation_not_matched"},
		{browser.LoginState{URL: "https://login.example.test/next"}, "https://mail.example.test/", "", "login_uncertain", "expectation_not_matched"},
		{browser.LoginState{URL: "https://x.example.test/", ExpectedText: true}, "", "Inbox", "login_uncertain", "expectation_not_matched"},
		{browser.LoginState{URL: "https://mail.example.test/inbox", ExpectedText: true, AuthenticatedUI: true}, "https://mail.example.test/inbox", "Signed in as user@example.test", "login_verified", ""},
		{browser.LoginState{URL: "https://mail.example.test/inbox", ExpectedText: true}, "https://mail.example.test/inbox", "Inbox", "login_uncertain", "expectation_not_matched"},
		{browser.LoginState{URL: "https://mail.example.test.evil.test/inbox", ExpectedText: true}, "https://mail.example.test", "Inbox", "login_uncertain", "expectation_not_matched"},
		{browser.LoginState{URL: "https://mail.example.test/inbox-failed", ExpectedText: true}, "https://mail.example.test/inbox", "Inbox", "login_uncertain", "expectation_not_matched"},
	}
	for _, c := range cases {
		login, why, _ := classifyLogin(c.state, c.url, c.expect != "")
		require.Equal(t, c.login, login, "%+v", c.state)
		require.Equal(t, c.why, why, "%+v", c.state)
	}
}

func TestTemporaryCredentialUsesAuthorizedFrameOrigin(t *testing.T) {
	session := "cred-frame-origin"
	sess := newSecretPage()
	sess.url = "https://parent.example.test/embedded-login"
	unsub := phone(t, session, func(r vault.CredentialRequest) {
		require.Equal(t, credOrigin, r.Origin)
		_, _ = vault.Ephemeral.Provide(r.ID, session, "phone-test", []byte(credSecret), false)
	})
	defer unsub()
	resp := runCredTool(t, t.Context(), sess, &mockPermissionService{}, session, BrowserParams{Action: "credential_request", Advanced: browser.Request{FrameID: "authorized-login-frame"}})
	require.False(t, resp.IsError, resp.Content)
	handle := metaOf(t, resp)["credential_id"].(string)
	fill := runCredTool(t, t.Context(), sess, &mockPermissionService{}, session, BrowserParams{Action: "vault_fill", CredentialID: handle})
	require.False(t, fill.IsError, fill.Content)
	require.Len(t, sess.fills, 1)
	require.Equal(t, credOrigin, metaOf(t, fill)["origin"])
}

func TestQuestionRefusesSecretRequests(t *testing.T) {
	t.Parallel()
	for text, secret := range map[string]bool{
		"What is your Gmail password?":          true,
		"Okul hesabının şifresi nedir?":         true,
		"Enter the OTP from your phone":         true,
		"Which account should I use?":           false,
		"Show the footprint of the build":       false,
		"Should I reset the password? (yes/no)": true,
	} {
		require.Equal(t, secret, asksForSecret(text), text)
	}
}

func TestHandoffMetadataIsWaitingForUser(t *testing.T) {
	t.Parallel()
	resp := handoffResponse("Approve the sign-in on your phone", "in the visible browser")
	require.True(t, resp.StopTurn)
	meta := metaOf(t, resp)
	require.Equal(t, "waiting_for_user", meta["status"])
	outcome, _ := meta["atlas_outcome"].(map[string]any)
	require.Equal(t, "waiting_for_user", outcome["claim"])
	require.NotContains(t, strings.ToLower(resp.Content), "return to the computer")
}
