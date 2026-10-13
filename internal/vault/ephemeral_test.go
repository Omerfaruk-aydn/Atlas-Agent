	}
}

func TestCredentialRefusedBeforeDispatchStaysUsable(t *testing.T) {
	t.Parallel()
	c, _, _ := newTestCredentials(t)
	req, _, err := c.Request(spec("s1"))
	require.NoError(t, err)
	_, err = c.Provide(req.ID, "s1", "phone-1", []byte(syntheticSecret), false)
	require.NoError(t, err)
	view, err := c.Use(req.ID, "s1", "https://login.example.test", "", func(string) (bool, error) { return false, errors.New("target_stale") })
	require.Error(t, err)
	require.Equal(t, CredentialProvided, view.State)
	// A new document requires a new human authorization, not silent rebinding.
	again, reused, err := c.Request(CredentialSpec{SessionID: "s1", Origin: "https://login.example.test", Account: "user@example.test", Target: CredentialTarget{TabID: "tab-1", Document: "doc-2", Selector: "#pw"}})
	require.NoError(t, err)
	require.False(t, reused)
	require.NotEqual(t, req.ID, again.ID)
	old, _ := c.Get(req.ID)
	require.Equal(t, CredentialCanceled, old.State)
	target, ok := c.Target(again.ID, "s1")
	require.True(t, ok)
	require.Equal(t, "doc-2", target.Document)
	_, err = c.Provide(again.ID, "s1", "phone-1", []byte(syntheticSecret), false)
	require.NoError(t, err)
	_, err = c.Use(again.ID, "s1", "https://login.example.test", "", func(string) (bool, error) { return true, nil })
	require.NoError(t, err)
}

func TestCredentialScopeIsBound(t *testing.T) {
	t.Parallel()
	c, _, _ := newTestCredentials(t)
	req, _, err := c.Request(spec("s1"))
	require.NoError(t, err)
	_, err = c.Provide(req.ID, "other-session", "phone-1", []byte(syntheticSecret), false)
	require.ErrorIs(t, err, ErrCredentialUnknown, "another task cannot answer this card")
	_, err = c.Provide(req.ID, "s1", "phone-1", []byte(syntheticSecret), false)
	require.NoError(t, err)
	never := func(string) (bool, error) { t.Fatal("dispatched outside the authorized scope"); return true, nil }
	_, err = c.Use(req.ID, "other-session", "https://login.example.test", "", never)
	require.ErrorIs(t, err, ErrCredentialScope)
	_, err = c.Use(req.ID, "s1", "https://evil.example.test", "", never)
	require.ErrorIs(t, err, ErrCredentialScope)
	_, err = c.Use(req.ID, "s1", "http://login.example.test", "", never)
	require.ErrorIs(t, err, ErrCredentialScope)
	_, err = c.Use(req.ID, "s1", "https://login.example.test", "someone-else@example.test", never)
	require.ErrorIs(t, err, ErrCredentialScope)
	_, err = c.Use("cr_unknown", "s1", "https://login.example.test", "", never)
	require.ErrorIs(t, err, ErrCredentialUnknown)
}

func TestCredentialExpiryCancelAndPurge(t *testing.T) {
	t.Parallel()
	c, clock, _ := newTestCredentials(t)
	req, _, err := c.Request(spec("s1"))
	require.NoError(t, err)
	clock.Add(CredentialRequestTTL + time.Second)
	_, err = c.Provide(req.ID, "s1", "phone-1", []byte(syntheticSecret), false)
	require.ErrorIs(t, err, ErrCredentialExpired)

	req2, _, err := c.Request(spec("s1"))
	require.NoError(t, err)
	require.NotEqual(t, req.ID, req2.ID)
	_, err = c.Provide(req2.ID, "s1", "phone-1", []byte(syntheticSecret), false)
	require.NoError(t, err)
	clock.Add(CredentialSecretTTL + time.Second)
	_, err = c.Use(req2.ID, "s1", "https://login.example.test", "", func(string) (bool, error) { t.Fatal("expired secret used"); return true, nil })
	require.ErrorIs(t, err, ErrCredentialExpired)

	req3, _, err := c.Request(spec("s1"))
	require.NoError(t, err)
	view, err := c.Cancel(req3.ID, "s1")
	require.NoError(t, err)
	require.Equal(t, CredentialCanceled, view.State)
	_, err = c.Provide(req3.ID, "s1", "phone-1", []byte(syntheticSecret), false)
	require.ErrorIs(t, err, ErrCredentialCanceled)

	req4, _, err := c.Request(spec("s2"))
	require.NoError(t, err)
	_, err = c.Provide(req4.ID, "s2", "phone-9", []byte(syntheticSecret), false)
	require.NoError(t, err)
	require.Equal(t, 1, c.PurgeDevice("phone-9"))
	got, _ := c.Get(req4.ID)
	require.Equal(t, CredentialCanceled, got.State)

	req5, _, err := c.Request(spec("s3"))
	require.NoError(t, err)
	require.Equal(t, 1, c.PurgeSession("s3"))
	got, _ = c.Get(req5.ID)
	require.Equal(t, CredentialCanceled, got.State)
}

func TestCredentialWaitWakesAndLastChannelCancels(t *testing.T) {
	t.Parallel()
	c := NewCredentials()
	unsub := c.Subscribe(func(CredentialEvent) {})
	req, _, err := c.Request(spec("s1"))
	require.NoError(t, err)
	done := make(chan CredentialRequest, 1)
	go func() {
		v, _ := c.Wait(context.Background(), req.ID)
		done <- v
	}()
	time.Sleep(20 * time.Millisecond)
	_, err = c.Provide(req.ID, "s1", "phone-1", []byte(syntheticSecret), false)
	require.NoError(t, err)
	select {
	case v := <-done:
		require.Equal(t, CredentialProvided, v.State)
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not wake on Provide")
	}

	req2, _, err := c.Request(CredentialSpec{SessionID: "s1", Origin: "https://other.example.test"})
	require.NoError(t, err)
	unsub()
	v, _ := c.Get(req2.ID)
	require.Equal(t, CredentialCanceled, v.State, "a card without any phone to answer it must not hang")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = c.Wait(ctx, req2.ID)
	require.NoError(t, err)
}

func TestCredentialProvideRegistersRedaction(t *testing.T) {
	t.Parallel()
	c, _, _ := newTestCredentials(t)
	session := "redact-" + t.Name()
	req, _, err := c.Request(spec(session))
	require.NoError(t, err)
	value := "Synth-Redact-" + strings.Repeat("x", 6)
	_, err = c.Provide(req.ID, session, "phone-1", []byte(value), false)
	require.NoError(t, err)
	require.True(t, Sensitive(session))
	require.NotContains(t, Redact(session, `{"value":"`+value+`"}`), value)
}

func TestCredentialOrigin(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"https://Accounts.Example.test":   "https://accounts.example.test",
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
