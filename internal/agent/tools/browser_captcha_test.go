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
