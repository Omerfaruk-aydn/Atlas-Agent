	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/stable/" && r.URL.RawQuery == "mode=json":
			_, _ = w.Write([]byte(`{"MSIsVersion":"1.104.1","MSIs":{"amd64":"` + name + `"}}`))
		case r.URL.Path == "/stable/"+name+".sha256":
			_, _ = w.Write([]byte(sum + "\n"))
		case r.URL.Path == "/stable/"+name:
			_, _ = w.Write(msi)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInstallerVerifiesBeforeRunning(t *testing.T) {
	t.Parallel()
	msi := []byte("fake msi payload")
	h := sha256.Sum256(msi)
	good := hex.EncodeToString(h[:])
	const name = "tailscale-setup-1.104.1-amd64.msi"

	run := func(t *testing.T, srv *httptest.Server, sig tsfunnel.Signature) (bool, error) {
		t.Helper()
		var ran bool
		in := tsfunnel.Installer{
			Client: srv.Client(), BaseURL: srv.URL + "/stable/", Arch: "amd64", TempRoot: t.TempDir(),
			Verify: func(_ context.Context, path string) (tsfunnel.Signature, error) {
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, msi, data)
				return sig, nil
			},
			Run: func(context.Context, string) error { ran = true; return nil },
		}
		return ran, in.Install(t.Context())
	}
	valid := tsfunnel.Signature{Status: "Valid", Subject: "CN=Tailscale Inc., O=Tailscale Inc."}

	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, good, name), valid)
		require.NoError(t, err)
		require.True(t, ran)
	})
	t.Run("hash mismatch", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, strings.Repeat("0", 64), name), valid)
		require.ErrorContains(t, err, "does not match")
		require.False(t, ran)
	})
	t.Run("missing hash", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, "", name), valid)
		require.Error(t, err)
		require.False(t, ran)
	})
	t.Run("bad signature", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, good, name), tsfunnel.Signature{Status: "HashMismatch", Subject: valid.Subject})
		require.ErrorContains(t, err, "not Valid")
		require.False(t, ran)
	})
	t.Run("wrong signer", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, good, name), tsfunnel.Signature{Status: "Valid", Subject: "CN=Someone Else"})
		require.ErrorContains(t, err, "not Tailscale")
		require.False(t, ran)
	})
	t.Run("suspicious name", func(t *testing.T) {
		t.Parallel()
		ran, err := run(t, packageServer(t, msi, good, `..\evil.msi`), valid)
		require.ErrorIs(t, err, tsfunnel.ErrMalformed)
		require.False(t, ran)
	})
	t.Run("plain http index", func(t *testing.T) {
		t.Parallel()
		in := tsfunnel.Installer{BaseURL: "http://pkgs.example/stable/", Verify: func(context.Context, string) (tsfunnel.Signature, error) { return valid, nil }, Run: func(context.Context, string) error { return errors.New("must not run") }}
		require.ErrorContains(t, in.Install(t.Context()), "https")
	})
}
