		"https://accounts.example.test/":  "https://accounts.example.test",
		"http://127.0.0.1:8080":           "http://127.0.0.1:8080",
		"http://localhost:3000":           "http://localhost:3000",
		"http://login.localhost":          "http://login.localhost",
		"http://accounts.example.test":    "",
		"https://user@accounts.example":   "",
		"https://accounts.example/path":   "",
		"https://accounts.example?next=1": "",
	} {
		got, err := CredentialOrigin(raw)
		if want == "" {
			require.Error(t, err, raw)
			continue
		}
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
}

func TestCredentialBudget(t *testing.T) {
	t.Parallel()
	c, _, _ := newTestCredentials(t)
	for i := range maxPendingPerSession {
		_, _, err := c.Request(CredentialSpec{SessionID: "s1", Origin: "https://a" + string(rune('a'+i)) + ".example.test"})
		require.NoError(t, err)
	}
	_, _, err := c.Request(CredentialSpec{SessionID: "s1", Origin: "https://zz.example.test"})
	require.ErrorIs(t, err, ErrCredentialBudget)
}
