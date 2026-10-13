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
