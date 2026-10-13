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
