	t.Parallel()
	chain := NewChain(MathSolver{})
	_, err := chain.Solve(context.Background(), Challenge{Kind: KindRecaptchaV2}, nil)
	require.ErrorIs(t, err, ErrNoSolver)
}

func TestChainRequiresImageForText(t *testing.T) {
	t.Parallel()
	chain := NewChain(MathSolver{})
	_, err := chain.Solve(context.Background(), Challenge{Kind: KindImageText, Question: "x"}, nil)
	require.Error(t, err)
}

func TestChainRejectsCheckbox(t *testing.T) {
	t.Parallel()
	chain := NewChain(MathSolver{})
	_, err := chain.Solve(context.Background(), Challenge{Kind: KindCheckbox}, nil)
	require.ErrorIs(t, err, ErrUnsupportedKind)
}

func TestBuildDefaultChain(t *testing.T) {
	t.Parallel()
	chain := Build(Options{Enabled: true})
	names := chain.Solvers()
	require.Contains(t, names, "math")
	require.True(t, chain.Supports(KindMath))
	require.False(t, chain.Supports(KindHCaptcha), "no remote key means no token solver")
}

func TestBuildRemoteChain(t *testing.T) {
	t.Parallel()
	chain := Build(Options{
		Enabled:        true,
		RemoteProvider: "2captcha",
		RemoteAPIKey:   "test-key",
	})
	require.True(t, chain.Supports(KindRecaptchaV2))
	require.True(t, chain.Supports(KindHCaptcha))
	require.True(t, chain.Supports(KindTurnstile))
}

func TestBuildDisabled(t *testing.T) {
	t.Parallel()
	chain := Build(Options{Enabled: false})
	require.Empty(t, chain.Solvers())
	require.False(t, chain.Supports(KindImageText))
}
