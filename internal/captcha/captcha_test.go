
func TestNormalizeAnswer(t *testing.T) {
	t.Parallel()
	require.Equal(t, "a1b2", NormalizeAnswer(" A 1 B 2 ", KindImageText))
	require.Equal(t, "42", NormalizeAnswer(" 42 ", KindMath))
	require.Equal(t, "xy7", NormalizeAnswer("xY7!", KindImageText))
}

func TestParseDetectRecaptcha(t *testing.T) {
	t.Parallel()
	raw := `{"kind":"recaptcha_v2","site_key":"6Le-w3UUAAAAA","question":"","confidence":0.9,"page_url":"https://example.com"}`
	d, ch, err := ParseDetect(raw, "https://example.com")
	require.NoError(t, err)
	require.True(t, d.Present())
	require.Equal(t, KindRecaptchaV2, ch.Kind)
	require.Equal(t, "6Le-w3UUAAAAA", ch.SiteKey)
}

func TestParseDetectImageText(t *testing.T) {
	t.Parallel()
	raw := `{"kind":"image_text","image_selector":"img.captcha","answer_selector":"#code","verify_selector":"button[type=submit]","confidence":0.95,"page_url":"https://ois.example/auth"}`
	_, ch, err := ParseDetect(raw, "")
	require.NoError(t, err)
	require.Equal(t, KindImageText, ch.Kind)
	require.Equal(t, "img.captcha", ch.ImageSelector)
	require.Equal(t, "#code", ch.AnswerSelector)
	require.True(t, ch.Kind.TextAnswer())
}

func TestParseDetectQuotedString(t *testing.T) {
	t.Parallel()
	raw := `"{\"kind\":\"math\",\"question\":\"3 + 4\"}"`
	_, ch, err := ParseDetect(raw, "https://x")
	require.NoError(t, err)
	require.Equal(t, KindMath, ch.Kind)
	require.Equal(t, "3 + 4", ch.Question)
}

func TestChainFallback(t *testing.T) {
	t.Parallel()
	fail := FuncSolver{
		NameStr: "fail",
		Accept:  func(k Kind) bool { return k == KindImageText },
		Fn: func(context.Context, Challenge, []byte) (Solution, error) {
			return Solution{}, errors.New("boom")
		},
	}
	ok := FuncSolver{
		NameStr: "ok",
		Accept:  func(k Kind) bool { return k == KindImageText },
		Fn: func(_ context.Context, _ Challenge, _ []byte) (Solution, error) {
			return Solution{Text: "a1b2c3"}, nil
		},
	}
	chain := NewChain(fail, ok).WithAttemptTimeout(2 * time.Second)
	var audited []Attempt
	chain = chain.WithAudit(func(a Attempt) { audited = append(audited, a) })

	res, err := chain.Solve(context.Background(), Challenge{Kind: KindImageText}, []byte{1})
	require.NoError(t, err)
	require.True(t, res.Solved)
	require.Equal(t, "a1b2c3", res.Solution.Text)
	require.Equal(t, "ok", res.Solution.Solver)
	require.Len(t, res.Attempts, 2)
	require.False(t, res.Attempts[0].OK)
	require.True(t, res.Attempts[1].OK)
	require.Len(t, audited, 2)
}

func TestChainNoSolver(t *testing.T) {
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
