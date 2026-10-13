			if ch.Kind.TextAnswer() && ch.AnswerSelector != "" {
				if _, fillErr := fillCaptchaAnswer(ctx, sess, ch.AnswerSelector, report.Answer); fillErr != nil {
					report.Message = "solved but fill failed: " + fillErr.Error()
					report.Next = "Type the answer into " + ch.AnswerSelector + " yourself."
					return browserJSONResponse(report), nil
				}
				report.Message = "solved and typed into " + ch.AnswerSelector
				if params.Advanced.Verify && ch.VerifySelector != "" {
					_ = sess.Click(ch.VerifySelector)
					report.Next = "Answer filled and verify clicked; call login_check."
				} else {
					report.Next = "Answer filled; submit the form then call login_check."
				}
				return browserJSONResponse(report), nil
			}
			if ch.Kind.TokenAnswer() {
				if ok, injectErr := injectCaptchaToken(ctx, sess, ch, report.Answer); injectErr == nil && ok {
					report.Message = "token injected"
					report.Next = "Submit the form then call login_check."
				} else {
					report.Next = "Token ready; inject it manually or retry."
				}
				return browserJSONResponse(report), nil
			}
			report.Next = "Answer ready; call captcha_fill to type it."
			return browserJSONResponse(report), nil
		}
	}

	// Nothing offline solved it: show the challenge crop to the model.
	if len(image) > 0 {
		return captchaChallengeImage(ch, image), nil
	}
	return browserJSONResponse(CaptchaResult{
		Status:         "unsolved",
		Kind:           string(ch.Kind),
		SiteKey:        ch.SiteKey,
		PageURL:        ch.PageURL,
		Question:       ch.Question,
		AnswerSelector: ch.AnswerSelector,
		VerifySelector: ch.VerifySelector,
		Message:        "No challenge image was captured and no solver could answer from the question text.",
		Next:           nextAfterUnsolved(ch),
	}), nil
}

// captchaChallengeImage returns the element-scoped challenge crop as an
// image response the model can look at. The captcha_challenge metadata
// mark lets it through the credential media gate: the crop contains only
// the public verification widget, never surrounding page content.
func captchaChallengeImage(ch captcha.Challenge, image []byte) fantasy.ToolResponse {
	resp := fantasy.NewImageResponse(image, "image/png")
	resp.Metadata = `{"captcha_challenge":true,"kind":"` + string(ch.Kind) + `"}`
	resp.Content = "CAPTCHA challenge (element crop only). Read the characters or expression in the image, " +
		"then call captcha_fill with the answer. " +
		"Selector: " + firstNonEmpty(ch.AnswerSelector, "(observe to find the answer field)") +
		". Do not describe the image; just read the answer and fill it."
	return resp
}

// runCaptchaFill types a model-supplied answer into the challenge's
// answer box. The answer is a public challenge response, not a user
// secret.
func runCaptchaFill(ctx context.Context, sess browser.Session, params BrowserParams) (fantasy.ToolResponse, error) {
	answer := strings.TrimSpace(firstNonEmpty(params.Value, params.Text, params.Advanced.Value))
	if answer == "" {
		return fantasy.NewTextErrorResponse("value (the CAPTCHA answer) is required for captcha_fill"), nil
	}
	selector := firstNonEmpty(params.Advanced.Selector, params.Selector)
	if selector == "" {
		// Fall back to a fresh detection so the caller does not need to
		// paste a selector back from the report.
		pageURL, _ := sess.URL()
		if raw, err := sess.Eval(captcha.DetectScript); err == nil {
			if _, ch, perr := captcha.ParseDetect(raw, pageURL); perr == nil {
				selector = ch.AnswerSelector
			}
		}
	}
	if selector == "" {
		return fantasy.NewTextErrorResponse("answer selector is required for captcha_fill; call captcha_detect first"), nil
	}
	if ok, err := fillCaptchaAnswer(ctx, sess, selector, answer); err != nil {
		return fantasy.NewTextErrorResponse("captcha fill failed: " + err.Error()), nil
	} else if !ok {
		return fantasy.NewTextErrorResponse("captcha fill could not reach the answer field " + selector), nil
	}
	next := "Answer typed into " + selector + "; submit the form then call login_check."
	if params.Advanced.Verify {
		verifySel := params.Advanced.VerifySelector
		verifyName := ""
		// Extract a human-readable name from jQuery-style selectors like
		// "button:contains('Giriş Yap')" so semantic_click can find the
		// button even when the selector is not valid CSS.
		if m := regexp.MustCompile(`:contains\(['"]?([^'")\]]+)['"]?\)`).FindStringSubmatch(verifySel); len(m) > 1 {
			verifyName = strings.TrimSpace(m[1])
		}
		if verifyName == "" && verifySel != "" && !strings.ContainsAny(verifySel, ":.#[]>()") {
			verifyName = verifySel // plain text like "Giriş Yap"
		}
		clicked := false
		if verifyName != "" {
			if driver, ok := sess.(interface {
				Input(ctx context.Context, p browser.Request) (browser.InputResult, error)
			}); ok {
				if _, err := driver.Input(ctx, browser.Request{Action: "semantic_click", Role: "button", Name: verifyName}); err == nil {
					clicked = true
				}
			}
		}
		if !clicked && verifySel != "" && !strings.Contains(verifySel, ":contains") {
			_ = sess.Click(verifySel)
			clicked = true
		}
		if !clicked {
			// Last resort: find the form's submit button by role.
			if driver, ok := sess.(interface {
				Input(ctx context.Context, p browser.Request) (browser.InputResult, error)
			}); ok {
				if _, err := driver.Input(ctx, browser.Request{Action: "semantic_click", Role: "button", Name: "Giriş Yap"}); err == nil {
					clicked = true
				}
			}
		}
		if clicked {
			next = "Answer typed and verify clicked; call login_check. If login_check still reports captcha, the answer was wrong: call captcha_solve again for a fresh image and retry."
		} else {
			next = "Answer typed but verify click failed. Click the submit button yourself (browser click with the button's ref), then call login_check."
		}
	}
	return browserJSONResponse(CaptchaResult{
		Status:         "filled",
		Kind:           string(captcha.KindImageText),
		Answer:         answer,
		AnswerSelector: selector,
		PageURL:        pageURLOf(sess),
		Next:           next,
	}), nil
}

func pageURLOf(sess browser.Session) string {
	u, _ := sess.URL()
	return u
}

func nextAfterUnsolved(ch captcha.Challenge) string {
	switch ch.Kind {
	case captcha.KindImageText, captcha.KindMath:
		return "Call captcha_solve again to get the challenge image, read it, then captcha_fill."
	case captcha.KindRecaptchaV2, captcha.KindRecaptchaV3, captcha.KindHCaptcha, captcha.KindTurnstile:
		return "Configure a remote token solver (tools.captcha.remote_provider + remote_api_key in atlas.json), or complete the widget by hand."
	default:
		return "Complete the verification widget by hand."
	}
}

// captureCaptchaImage screenshots only the challenge element. It never
// returns a full-page capture, so surrounding form fields (and any
// secrets) stay out of the crop.
func captureCaptchaImage(ctx context.Context, sess browser.Session, ch captcha.Challenge) ([]byte, error) {
	if ch.ImageSelector != "" {
		boxRaw, err := sess.Eval(fmt.Sprintf(`(() => {
const e = document.querySelector(%q);
if (!e) return null;
const r = e.getBoundingClientRect();
return {x: r.x + scrollX, y: r.y + scrollY, w: r.width, h: r.height};
})()`, ch.ImageSelector))
		if err != nil {
			return nil, err
		}
		var box captcha.DOMRect
		if err := json.Unmarshal([]byte(strings.TrimSpace(boxRaw)), &box); err == nil && box.W >= 4 && box.H >= 4 {
			if data, err := captureClip(sess, box); err == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	dataRaw, err := sess.Eval(fmt.Sprintf(`(() => {
const e = document.querySelector(%q);
if (!e) return "";
if (e.tagName === "IMG" && e.complete && e.naturalWidth > 0) {
  const c = document.createElement("canvas");
  c.width = e.naturalWidth; c.height = e.naturalHeight;
  c.getContext("2d").drawImage(e, 0, 0);
  return c.toDataURL("image/png");
}
if (e.tagName === "CANVAS") return e.toDataURL("image/png");
return "";
})()`, firstNonEmpty(ch.ImageSelector, "img")))
	if err == nil {
		s := strings.TrimSpace(strings.Trim(dataRaw, `"`))
		if strings.HasPrefix(s, "data:image/png;base64,") {
			if raw, decErr := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "data:image/png;base64,")); decErr == nil && len(raw) > 0 {
				return raw, nil
			}
		}
	}
	return nil, fmt.Errorf("challenge image element %q could not be captured", ch.ImageSelector)
}

// captureClip captures a clipped screenshot through CDP so only the
// challenge rect leaves the browser process. It uses 2x device pixel ratio
// for crisper images that the model can read more accurately.
func captureClip(sess browser.Session, box captcha.DOMRect) ([]byte, error) {
	raw, ok := sess.(interface {
		RawCDP(method string, params map[string]any) (map[string]any, error)
	})
	if !ok {
		return nil, fmt.Errorf("session does not support clipped capture")
	}
	dprRaw, err := sess.Eval("window.devicePixelRatio || 1")
	if err != nil {
		return nil, err
	}
	dpr, _ := strconv.ParseFloat(strings.TrimSpace(strings.Trim(dprRaw, `"`)), 64)
	if dpr <= 0 {
		dpr = 1
	}
	// Use at least 2x for clarity; if the display is already 2x+, keep it.
	captureScale := dpr
	if captureScale < 2.0 {
		captureScale = 2.0
	}
	const pad = 2.0
	result, err := raw.RawCDP("Page.captureScreenshot", map[string]any{
		"format": "png",
		"clip": map[string]any{
			"x":      (box.X - pad) * dpr,
			"y":      (box.Y - pad) * dpr,
			"width":  (box.W + 2*pad) * dpr,
			"height": (box.H + 2*pad) * dpr,
			"scale":  captureScale,
		},
		"captureBeyondViewport": true,
	})
	if err != nil {
		return nil, err
	}
	data, _ := result["data"].(string)
	if data == "" {
		return nil, fmt.Errorf("clipped capture returned no data")
	}
	return base64.StdEncoding.DecodeString(data)
}

// fillCaptchaAnswer types the solved text into the answer box and commits
// it. The answer is a challenge response, not a user secret.
func fillCaptchaAnswer(ctx context.Context, sess browser.Session, selector, answer string) (bool, error) {
	if selector == "" || answer == "" {
		return false, fmt.Errorf("answer selector and answer are required")
	}
	clear := fmt.Sprintf(`(() => {
const e = document.querySelector(%q);
if (!e) return false;
e.focus();
if ("value" in e) e.value = "";
e.dispatchEvent(new Event("input", {bubbles: true}));
return true;
})()`, selector)
	if _, err := sess.Eval(clear); err != nil {
		return false, err
	}
	if err := sess.Type(selector, answer); err != nil {
		set := fmt.Sprintf(`(() => {
const e = document.querySelector(%q);
if (!e) return false;
e.focus();
if ("value" in e) e.value = %q;
e.dispatchEvent(new Event("input", {bubbles: true}));
e.dispatchEvent(new Event("change", {bubbles: true}));
e.dispatchEvent(new Event("blur", {bubbles: true}));
return true;
})()`, selector, answer)
		okRaw, evalErr := sess.Eval(set)
		if evalErr != nil {
			return false, err
		}
		return strings.Contains(okRaw, "true"), nil
	}
	return true, nil
}

// injectCaptchaToken places a provider token into the standard response
// fields and fires the widget callback when one is present.
func injectCaptchaToken(ctx context.Context, sess browser.Session, ch captcha.Challenge, token string) (bool, error) {
	if token == "" {
		return false, fmt.Errorf("empty token")
	}
	script := fmt.Sprintf(`(() => {
const token = %q;
const sel = ["textarea[name=g-recaptcha-response]", "input[name=g-recaptcha-response]",
  "textarea[name=h-captcha-response]", "input[name=h-captcha-response]",
  "textarea[name=cf-turnstile-response]", "input[name=cf-turnstile-response]",
  "[name=g-recaptcha-response]", "[name=h-captcha-response]", "[name=cf-turnstile-response]"];
let filled = false;
for (const s of sel) {
  for (const e of document.querySelectorAll(s)) {
    e.value = token;
    filled = true;
  }
}
if (!filled) {
  const ta = document.createElement("textarea");
  ta.name = "g-recaptcha-response";
  ta.style.display = "none";
  ta.value = token;
  (document.body || document.documentElement).appendChild(ta);
  filled = true;
}
try {
  if (typeof ___grecaptcha_cfg !== "undefined") {
    const walk = (o, depth) => {
      if (depth > 6 || !o || typeof o !== "object") return;
      for (const k of Object.keys(o)) {
        const v = o[k];
        if (v && typeof v === "function" && k === "callback") { try { v(token); } catch {} }
        else walk(v, depth + 1);
      }
    };
    walk(___grecaptcha_cfg, 0);
  }
} catch {}
return filled;
})()`, token)
	raw, err := sess.Eval(script)
	if err != nil {
		return false, err
	}
	return strings.Contains(raw, "true"), nil
}

// silence unused import when helpers move.
var _ = time.Second

// abs returns the absolute value of an int.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
