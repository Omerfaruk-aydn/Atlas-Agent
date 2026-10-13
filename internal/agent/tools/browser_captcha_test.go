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
