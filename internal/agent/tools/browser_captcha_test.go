	c.clicked = sel
	return nil
}

func newCaptchaSession(url string, eval map[string]string) *captchaSession {
	return &captchaSession{
		fakeBrowserSession: &fakeBrowserSession{url: url},
		evalOut:            eval,
	}
}

func withCaptchaChain(t *testing.T, c *captcha.Chain) {
	t.Helper()
	prev := captchaChain
	t.Cleanup(func() { captchaChain = prev })
	SetCaptchaChain(c)
}

func TestCaptchaDetectNone(t *testing.T) {
	t.Parallel()
	sess := newCaptchaSession("https://example.com", map[string]string{
		captcha.DetectScript: `{"kind":"none","confidence":0}`,
	})
	resp, err := runCaptchaDetect(context.Background(), sess)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	var out CaptchaResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &out))
	require.Equal(t, "none", out.Status)
}

func TestCaptchaDetectImageText(t *testing.T) {
	t.Parallel()
	sess := newCaptchaSession("https://ois.example/auth", map[string]string{
		captcha.DetectScript: `{"kind":"image_text","image_selector":"img.captcha","answer_selector":"#code","verify_selector":"button[type=submit]","confidence":0.95,"question":"Güvenlik Resmi","page_url":"https://ois.example/auth"}`,
	})
	resp, err := runCaptchaDetect(context.Background(), sess)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	var out CaptchaResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &out))
	require.Equal(t, "detected", out.Status)
	require.Equal(t, "image_text", out.Kind)
	require.Equal(t, "#code", out.AnswerSelector)
	require.NotEmpty(t, out.Message)
}

func TestCaptchaSolveCheckbox(t *testing.T) {
	t.Parallel()
	sess := newCaptchaSession("https://example.com", map[string]string{
		captcha.DetectScript: `{"kind":"checkbox","answer_selector":"#robot","confidence":0.85}`,
	})
	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_solve", BrowserParams{Action: "captcha_solve"}, "sess1")
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	var out CaptchaResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &out))
	require.Equal(t, "needs_click", out.Status)
	require.Empty(t, resp.Data, "checkbox path returns text only")
}

func TestCaptchaSolveMathOffline(t *testing.T) {
	withCaptchaChain(t, captcha.NewChain(captcha.MathSolver{}).WithAttemptTimeout(5*time.Second))

	sess := newCaptchaSession("https://example.com", map[string]string{
		captcha.DetectScript:                `{"kind":"math","question":"7 + 5","answer_selector":"#answer","verify_selector":"#go","confidence":0.9}`,
		`document.querySelector("#answer")`: "true",
	})
	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_solve", BrowserParams{Action: "captcha_solve"}, "sess1")
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	var out CaptchaResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &out))
	require.Equal(t, "solved", out.Status)
	require.Equal(t, "12", out.Answer)
	require.Equal(t, "math", out.Solver)
	require.Equal(t, "#answer", sess.typedSel)
	require.Equal(t, "12", sess.typedText)
}

func TestCaptchaSolveImageTextReturnsImageToModel(t *testing.T) {
	// No solver answers image_text, so the challenge crop must be handed
	// to the model -- NOT sent to an external API. The response carries
	// image data plus the captcha_challenge marker so the credential
	// media gate lets it through.
	withCaptchaChain(t, captcha.NewChain(captcha.MathSolver{}))

	sess := newCaptchaSession("https://ois.example/auth", map[string]string{
		captcha.DetectScript: `{"kind":"image_text","image_selector":"img.captcha","answer_selector":"#code","confidence":0.9,"question":"Güvenlik Resmi"}`,
	})
	sess.evalFn = func(script string) (string, error) {
		switch {
		case script == captcha.DetectScript:
			return `{"kind":"image_text","image_selector":"img.captcha","answer_selector":"#code","confidence":0.9,"question":"Güvenlik Resmi"}`, nil
		case strings.Contains(script, "getBoundingClientRect"):
			return `{"x":10,"y":20,"w":120,"h":40}`, nil
		case strings.Contains(script, "devicePixelRatio"):
			return `1`, nil
		case strings.Contains(script, "toDataURL"):
			return `data:image/png;base64,iVBORw0KGgo=`, nil
		}
		return "null", nil
	}
	sess.cdpResult = map[string]any{"data": "iVBORw0KGgo="}

	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_solve", BrowserParams{Action: "captcha_solve"}, "sess1")
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.NotEmpty(t, resp.Data, "model must receive the challenge crop")
	require.True(t, strings.Contains(resp.Metadata, `"captcha_challenge":true`),
		"metadata must mark the crop as a captcha challenge so the media gate allows it; got %s", resp.Metadata)
	require.Contains(t, resp.Content, "captcha_fill")
}

func TestCaptchaSolveMathWithVerify(t *testing.T) {
	withCaptchaChain(t, captcha.NewChain(captcha.MathSolver{}).WithAttemptTimeout(5*time.Second))

	sess := newCaptchaSession("https://example.com", map[string]string{
		captcha.DetectScript:           `{"kind":"math","question":"3 * 4","answer_selector":"#a","verify_selector":"#go","confidence":0.9}`,
		`document.querySelector("#a")`: "true",
	})
	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_solve", BrowserParams{
		Action:   "captcha_solve",
		Advanced: browser.Request{Verify: true},
	}, "sess1")
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	var out CaptchaResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &out))
	require.Equal(t, "solved", out.Status)
	require.Equal(t, "12", out.Answer)
	require.Contains(t, sess.clicks, "#go")
	require.Contains(t, out.Next, "login_check")
}

func TestCaptchaFill(t *testing.T) {
	t.Parallel()
	sess := newCaptchaSession("https://ois.example/auth", map[string]string{
		captcha.DetectScript:              `{"kind":"image_text","answer_selector":"#code","confidence":0.9}`,
		`document.querySelector("#code")`: "true",
	})
	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_fill", BrowserParams{
		Action: "captcha_fill",
		Value:  "a7k2m",
	}, "sess1")
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	var out CaptchaResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &out))
	require.Equal(t, "filled", out.Status)
	require.Equal(t, "a7k2m", out.Answer)
	require.Equal(t, "#code", sess.typedSel)
	require.Equal(t, "a7k2m", sess.typedText)
}

func TestCaptchaFillRequiresAnswer(t *testing.T) {
	t.Parallel()
	sess := newCaptchaSession("https://example.com", nil)
	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_fill", BrowserParams{Action: "captcha_fill"}, "s")
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "required")
}

func TestCaptchaMediaGateAllowsChallengeCrops(t *testing.T) {
	t.Parallel()
	// The media gate must let captcha challenge images through even in
	// credential-bearing sessions. This is the whole point of the
	// redesign: the model reads the CAPTCHA itself.
	require.True(t, isCaptchaChallengeMedia(fantasy.ToolResponse{
		Type:     "image",
		Metadata: `{"captcha_challenge":true,"kind":"image_text"}`,
		Data:     []byte{1},
	}))
	require.False(t, isCaptchaChallengeMedia(fantasy.ToolResponse{
		Type:     "image",
		Metadata: `{"viewport_id":"x"}`,
		Data:     []byte{1},
	}), "ordinary screenshots must stay blocked")
	require.False(t, isCaptchaChallengeMedia(fantasy.ToolResponse{
		Type: "text",
		Data: []byte{1},
	}), "non-image payloads are not captcha crops")
}

func TestCaptchaActionsRegistered(t *testing.T) {
	t.Parallel()
	require.True(t, captchaActions["captcha_detect"])
	require.True(t, captchaActions["captcha_solve"])
	require.True(t, captchaActions["captcha_fill"])
	require.Contains(t, browserActions, "captcha_detect")
	require.Contains(t, browserActions, "captcha_solve")
	require.Contains(t, browserActions, "captcha_fill")
}

func TestCaptchaDetectInteractiveTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		detect   string
		wantKind string
	}{
		{"click", `{"kind":"click","image_selector":"img.click-captcha","confidence":0.85,"question":"click the icons in order"}`, "click"},
		{"rotate", `{"kind":"rotate","image_selector":"img.rotate","confidence":0.9,"question":"rotate to correct orientation"}`, "rotate"},
		{"slider", `{"kind":"slider","slider_selector":".geetest_slider_button","confidence":0.9,"question":"drag slider to gap"}`, "slider"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sess := newCaptchaSession("https://demo.test", map[string]string{
				captcha.DetectScript: c.detect,
			})
			resp, err := runCaptchaDetect(context.Background(), sess)
			require.NoError(t, err)
			require.False(t, resp.IsError)
			var out CaptchaResult
			require.NoError(t, json.Unmarshal([]byte(resp.Content), &out))
			require.Equal(t, c.wantKind, out.Kind)
		})
	}
}

func TestCaptchaSolveClickReturnsImageWithInstructions(t *testing.T) {
	withCaptchaChain(t, captcha.NewChain(captcha.MathSolver{}))
	sess := newCaptchaSession("https://demo.test", nil)
	sess.evalFn = func(script string) (string, error) {
		switch {
		case script == captcha.DetectScript:
			return `{"kind":"click","image_selector":"img.click","confidence":0.9,"question":"click icons in order"}`, nil
		case strings.Contains(script, "getBoundingClientRect"):
			return `{"x":10,"y":10,"w":200,"h":150}`, nil
		case strings.Contains(script, "devicePixelRatio"):
			return `1`, nil
		case strings.Contains(script, "toDataURL"):
			return `data:image/png;base64,iVBORw0KGgo=`, nil
		}
		return "null", nil
	}
	sess.cdpResult = map[string]any{"data": "iVBORw0KGgo="}
	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_solve", BrowserParams{Action: "captcha_solve"}, "s")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Data, "click captcha must return the challenge image")
	require.Contains(t, resp.Metadata, `"captcha_challenge":true`)
	require.Contains(t, resp.Content, "CLICK-SEQUENCE")
	require.Contains(t, resp.Content, "pointer_click")
}

func TestCaptchaSolveRotateReturnsImageWithInstructions(t *testing.T) {
	withCaptchaChain(t, captcha.NewChain(captcha.MathSolver{}))
	sess := newCaptchaSession("https://demo.test", nil)
	sess.evalFn = func(script string) (string, error) {
		switch {
		case script == captcha.DetectScript:
			return `{"kind":"rotate","image_selector":"img.rotate","confidence":0.9,"rotation_hint":90}`, nil
		case strings.Contains(script, "RotateAngleScript") || strings.Contains(script, "estimatedAngle"):
			return `{"found":true,"estimatedAngle":45,"confidence":0.8}`, nil
		case strings.Contains(script, "getBoundingClientRect"):
			return `{"x":5,"y":5,"w":150,"h":150}`, nil
		case strings.Contains(script, "devicePixelRatio"):
			return `1`, nil
		case strings.Contains(script, "toDataURL"):
			return `data:image/png;base64,iVBORw0KGgo=`, nil
		}
		return "null", nil
	}
	sess.cdpResult = map[string]any{"data": "iVBORw0KGgo="}
	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_solve", BrowserParams{Action: "captcha_solve"}, "s")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Data)
	require.Contains(t, resp.Content, "ROTATE")
	require.Contains(t, resp.Content, "EXACT ANGLE")
	require.Contains(t, resp.Content, "45")
}

func TestCaptchaSolveSliderReturnsImageWithInstructions(t *testing.T) {
	withCaptchaChain(t, captcha.NewChain(captcha.MathSolver{}))
	sess := newCaptchaSession("https://demo.test", nil)
	sess.evalFn = func(script string) (string, error) {
		switch {
		case script == captcha.DetectScript:
			return `{"kind":"slider","slider_selector":".geetest_slider","confidence":0.9}`, nil
		case strings.Contains(script, "GapDetectScript") || strings.Contains(script, "gapX"):
			return `{"found":true,"gapX":180,"gapRatio":0.6,"dragDistance":120,"canvasWidth":300,"canvasHeight":200,"method":"edge"}`, nil
		case strings.Contains(script, "getBoundingClientRect"):
			return `{"x":0,"y":0,"w":300,"h":200}`, nil
		case strings.Contains(script, "devicePixelRatio"):
			return `1`, nil
		case strings.Contains(script, "toDataURL"):
			return `data:image/png;base64,iVBORw0KGgo=`, nil
		}
		return "null", nil
	}
	sess.cdpResult = map[string]any{"data": "iVBORw0KGgo="}
	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_solve", BrowserParams{Action: "captcha_solve"}, "s")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Data)
	require.Contains(t, resp.Content, "SLIDER")
	require.Contains(t, resp.Content, "GAP DETECTION")
	require.Contains(t, resp.Content, "120")
}

func TestCaptchaSolveSliderGapDetectionFallback(t *testing.T) {
	withCaptchaChain(t, captcha.NewChain(captcha.MathSolver{}))
	sess := newCaptchaSession("https://demo.test", nil)
	sess.evalFn = func(script string) (string, error) {
		switch {
		case script == captcha.DetectScript:
			return `{"kind":"slider","slider_selector":".geetest_slider","confidence":0.9}`, nil
		case strings.Contains(script, "GapDetectScript") || strings.Contains(script, "gapX"):
			return `{"found":false,"reason":"no background canvas"}`, nil
		case strings.Contains(script, "getBoundingClientRect"):
			return `{"x":0,"y":0,"w":300,"h":200}`, nil
		case strings.Contains(script, "devicePixelRatio"):
			return `1`, nil
		case strings.Contains(script, "toDataURL"):
			return `data:image/png;base64,iVBORw0KGgo=`, nil
		}
		return "null", nil
	}
	sess.cdpResult = map[string]any{"data": "iVBORw0KGgo="}
	resp, err := runBrowserCaptcha(context.Background(), sess, "captcha_solve", BrowserParams{Action: "captcha_solve"}, "s")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Data)
	require.Contains(t, resp.Content, "SLIDER")
	require.Contains(t, resp.Content, "css_drag")
	require.Contains(t, resp.Content, "CALIBRATION")
}

func TestCaptchaDetectScriptMentionsInteractiveTypes(t *testing.T) {
	t.Parallel()
	s := captcha.DetectScript
	for _, needle := range []string{"click", "rotate", "slider", "geetest", "döndür", "sürükle"} {
		require.Contains(t, s, needle)
	}
}
