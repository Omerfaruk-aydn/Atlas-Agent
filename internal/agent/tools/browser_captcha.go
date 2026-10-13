	case captcha.KindCheckbox:
		return "Checkbox widget: a single click usually clears it (use browser click)."
	case captcha.KindClick:
		return "Click-sequence CAPTCHA: captcha_solve returns the challenge image; identify the icons and click them in the correct order using pointer_click or click."
	case captcha.KindRotate:
		return "Rotate CAPTCHA: captcha_solve returns the image; determine the correct orientation and use pointer_drag to rotate it, then click Verify."
	case captcha.KindSlider:
		return "Slider puzzle CAPTCHA: captcha_solve returns the puzzle image; find the gap and drag the slider to fill it using pointer_drag, then release."
	case captcha.KindKeyCaptcha:
		return "KeyCAPTCHA widget: captcha_solve returns the widget image; find the arrow/button and click or drag it to the correct position."
	case captcha.KindRecaptchaV2, captcha.KindRecaptchaV3, captcha.KindHCaptcha, captcha.KindTurnstile:
		return "Token widget (" + string(ch.Kind) + "): needs a remote token solver, or the user completes it by hand."
	default:
		return "Unrecognized verification widget."
	}
}

// handleInteractiveCaptcha returns the challenge image and type-specific
// interaction instructions for click-sequence, rotate and slider puzzles.
// For slider puzzles it runs gap detection; for rotate it estimates the angle.
func handleInteractiveCaptcha(ctx context.Context, sess browser.Session, ch captcha.Challenge, params BrowserParams) (fantasy.ToolResponse, error) {
	// For slider puzzles: run gap detection first to get the exact position.
	var gapInfo *GapDetectResult
	if ch.Kind == captcha.KindSlider {
		if raw, err := sess.Eval(captcha.GapDetectScript); err == nil {
			var g GapDetectResult
			if json.Unmarshal([]byte(strings.TrimSpace(raw)), &g) == nil && g.Found {
				gapInfo = &g
			}
		}
	}

	// For rotate puzzles: estimate the current rotation angle.
	var rotateInfo *RotateAngleResult
	if ch.Kind == captcha.KindRotate {
		if raw, err := sess.Eval(captcha.RotateAngleScript); err == nil {
			var r RotateAngleResult
			if json.Unmarshal([]byte(strings.TrimSpace(raw)), &r) == nil && r.Found {
				rotateInfo = &r
			}
		}
	}

	// Capture the challenge area at high resolution. For interactive
	// captchas, capture the full widget container (not just the image
	// element) so the model can see buttons, slider handle, and the
	// puzzle together.
	var image []byte
	if ch.ImageSelector != "" || ch.ClickTargetSelector != "" || ch.SliderSelector != "" {
		// Try to find the widget container first (parent of the image or
		// slider) for a more complete capture.
		containerSel := firstNonEmpty(ch.SliderSelector, ch.ClickTargetSelector, ch.ImageSelector)
		containerScript := fmt.Sprintf(`(() => {
const e = document.querySelector(%q);
if (!e) return null;
// Walk up to find a container that includes both the image and the
// interactive controls (max 3 levels).
let c = e;
for (let i = 0; i < 3 && c.parentElement; i++) {
  const p = c.parentElement;
  if (p.querySelector('button, [class*="slider"], [class*="rotate"], [class*="verify"]') || p.offsetHeight > c.offsetHeight * 1.3) {
    c = p;
  } else break;
}
const r = c.getBoundingClientRect();
return {x: r.x + scrollX, y: r.y + scrollY, w: r.width, h: r.height};
})()`, containerSel)
		if boxRaw, err := sess.Eval(containerScript); err == nil {
			var box captcha.DOMRect
			if json.Unmarshal([]byte(strings.TrimSpace(boxRaw)), &box) == nil && box.W >= 10 && box.H >= 10 {
				if img, err := captureClip(sess, box); err == nil && len(img) > 0 {
					image = img
				}
			}
		}
		if len(image) == 0 {
			img, err := captureCaptchaImage(ctx, sess, ch)
			if err == nil {
				image = img
			}
		}
	}
	if len(image) == 0 {
		// Capture the whole challenge widget region from the detected rect.
		if d, err := sess.Eval(captcha.DetectScript); err == nil {
			if det, _, perr := captcha.ParseDetect(d, ""); perr == nil && det.Rect != nil {
				if img, err := captureClip(sess, *det.Rect); err == nil {
					image = img
				}
			}
		}
	}
	if len(image) == 0 {
		// Last resort: clean screenshot of the viewport.
		if img, err := sess.Screenshot(false); err == nil {
			image = img
		}
	}
	if len(image) == 0 {
		return browserJSONResponse(CaptchaResult{
			Status:  "unsolved",
			Kind:    string(ch.Kind),
			PageURL: ch.PageURL,
			Message: "Could not capture the challenge image.",
			Next:    "Take a screenshot manually and solve it, or ask the user.",
		}), nil
	}

	resp := fantasy.NewImageResponse(image, "image/png")
	resp.Metadata = `{"captcha_challenge":true,"kind":"` + string(ch.Kind) + `"}`

	switch ch.Kind {
	case captcha.KindClick:
		resp.Content = "CLICK-SEQUENCE CAPTCHA. Look at the image and identify the icons in the order they must be clicked. " +
			"Then use pointer_click with viewport_id from an annotated screenshot to click each icon in order. " +
			"Click targets: " + firstNonEmpty(ch.ClickTargetSelector, "(use screenshot coordinates)") + ". " +
			"After clicking all icons in order, click Verify/Submit."
	case captcha.KindRotate:
		instructions := "ROTATE CAPTCHA. "
		clickDeg := 15
		if rotateInfo != nil && rotateInfo.Found {
			if rotateInfo.ClickDegrees > 0 {
				clickDeg = rotateInfo.ClickDegrees
			}
			angle := rotateInfo.EstimatedAngle
			if rotateInfo.DeclaredAngle != nil {
				angle = int(*rotateInfo.DeclaredAngle)
			}
			instructions += fmt.Sprintf("EXACT ANGLE from CSS transform: %d° (each click = %d°). ", angle, clickDeg)
			if angle > 0 {
				instructions += fmt.Sprintf("Current: +%d° (clockwise). To reach 0°: click 'Rotate LEFT' %d times. ", angle, abs(angle)/clickDeg)
			} else if angle < 0 {
				instructions += fmt.Sprintf("Current: %d° (counter-clockwise). To reach 0°: click 'Rotate RIGHT' %d times. ", angle, abs(angle)/clickDeg)
			} else {
				instructions += "Already at 0° — try Check. "
			}
		} else {
			instructions += "Determine rotation visually. Each click = 15°. "
		}
		instructions += "\nCRITICAL: click/semantic_click may fail with 'target_changed_before_dispatch'. Use eval instead:\n" +
			"Rotate left: eval: [...document.querySelectorAll('button')].find(b=>b.textContent.includes('Rotate left')||b.textContent.includes('Rotate Left'))?.click()\n" +
			"Rotate right: eval: [...document.querySelectorAll('button')].find(b=>b.textContent.includes('Rotate right')||b.textContent.includes('Rotate Right'))?.click()\n" +
			"Check: eval: [...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='Check')?.click()\n" +
			"After each click, verify angle: eval: [...document.querySelectorAll('img')].find(e=>e.src.includes('captcha-')&&e.getBoundingClientRect().width>100)?.style.transform\n" +
			"After Check, read result: eval: document.body.innerText.match(/Incorrect|Correct|Success|solved/gi)\n" +
			"IMPORTANT: The image may have a BAKED-IN starting rotation. The CSS transform shows only the ADDED rotation. " +
			"The correct answer is the angle that makes the image upright — you MUST verify visually after each step.\n" +
			"Target: " + firstNonEmpty(ch.SliderSelector, "(Rotate buttons)")
		resp.Content = instructions
	case captcha.KindSlider:
		instructions := "SLIDER PUZZLE CAPTCHA (GeeTest). "
		if gapInfo != nil && gapInfo.Found {
			instructions += fmt.Sprintf("GAP DETECTION: Gap at x≈%d (canvas %dpx). ", gapInfo.GapX, gapInfo.CanvasWidth)
			if gapInfo.PieceRange != nil {
				instructions += fmt.Sprintf("Piece at cols %d-%d. ", gapInfo.PieceRange.Min, gapInfo.PieceRange.Max)
			}
			if gapInfo.DragDistance > 0 {
				instructions += fmt.Sprintf("Estimated drag: ~%d CSS px. ", int(gapInfo.DragDistance))
			}
		}
		instructions += "\nCALIBRATION-FIRST STRATEGY (do NOT guess distances):\n" +
			"1. Get knob center in CSS pixels:\n" +
			"   eval: (()=>{const b=document.querySelector('.geetest_slider_button');const r=b.getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()\n" +
			"2. CALIBRATE: css_drag small 50px, then measure piece movement:\n" +
			`   {"action":"css_drag","advanced":{"x":751,"y":443,"to_x":801,"to_y":443,"steps":10,"hold_ms":50}}` + "\n" +
			"   (replace 751/443 with your eval result; these are CSS pixels, NOT screenshot pixels)\n" +
			"   eval: measure pieceMinX from slice canvas before and after\n" +
			"   ratio = pieceMove / knobMove (typically ~1.0)\n" +
			"3. FIND GAP: analyze bg canvas luminance — skip piece's own shadow (x=0-N)\n" +
			"4. CALCULATE: dragDistance = (gapX - pieceMinX) / ratio\n" +
			"5. DRAG: css_drag with calculated distance\n" +
			"6. VERIFY: eval: document.querySelector('.geetest_btn')?.innerText\n" +
			"If 'Incomplete', adjust by 5px and retry. Max 3 attempts.\n" +
			"CRITICAL: css_drag uses CSS pixels (from getBoundingClientRect), NOT screenshot pixels. Do NOT pass viewport_id.\n" +
			"Slider: " + firstNonEmpty(ch.SliderSelector, ".geetest_slider_button")
		resp.Content = instructions
	case captcha.KindKeyCaptcha:
		resp.Content = "KEYCAPTCHA. Look at the widget image. If the widget is empty or not loaded, report that the widget failed to render and move on. " +
			"If visible, find the interactive element (arrow, button, or draggable piece) and interact with it using pointer_click or pointer_drag. " +
			"Target: " + firstNonEmpty(ch.SliderSelector, "(look for the interactive element in the widget)") + ". " +
			"After interacting, click Verify/Check."
	}
	return resp, nil
}

// GapDetectResult holds the output of GapDetectScript.
type GapDetectResult struct {
	Found        bool    `json:"found"`
	Method       string  `json:"method"`
	Reason       string  `json:"reason"`
	GapX         int     `json:"gapX"`
	GapRatio     float64 `json:"gapRatio"`
	GapCSSX      float64 `json:"gapCSSX"`
	DragDistance float64 `json:"dragDistance"`
	SliderCSSX   float64 `json:"sliderCSSX"`
	CanvasWidth  int     `json:"canvasWidth"`
	CanvasHeight int     `json:"canvasHeight"`
	Profile      []int   `json:"profile"`
	PieceRange   *struct {
		Min   int `json:"min"`
		Max   int `json:"max"`
		Width int `json:"width"`
	} `json:"pieceRange"`
}

// RotateAngleResult holds the output of RotateAngleScript.
type RotateAngleResult struct {
	Found          bool     `json:"found"`
	EstimatedAngle int      `json:"estimatedAngle"`
	DeclaredAngle  *float64 `json:"declaredAngle"`
	Confidence     float64  `json:"confidence"`
	Reason         string   `json:"reason"`
	ClickDegrees   int      `json:"clickDegrees"`
}

// runCaptchaSolve answers what it can offline (math), then hands the
// challenge crop to the model. The crop is element-scoped and carries the
// captcha_challenge metadata mark so the credential media gate lets it
// through: a CAPTCHA picture is a public widget, not a user secret.
func runCaptchaSolve(ctx context.Context, sess browser.Session, params BrowserParams) (fantasy.ToolResponse, error) {
	pageURL, _ := sess.URL()
	raw, err := sess.Eval(captcha.DetectScript)
	if err != nil {
		return fantasy.NewTextErrorResponse("captcha detection failed: " + err.Error()), nil
	}
	detected, ch, err := captcha.ParseDetect(raw, pageURL)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if !detected.Present() {
		return browserJSONResponse(CaptchaResult{
			Status:  "none",
			PageURL: pageURL,
			Next:    "No verification widget is visible; nothing to solve.",
		}), nil
	}
	if ch.Kind == captcha.KindCheckbox {
		return browserJSONResponse(CaptchaResult{
			Status:  "needs_click",
			Kind:    string(ch.Kind),
			PageURL: ch.PageURL,
			Next:    "Click the checkbox with the browser tool, then call login_check.",
		}), nil
	}

	// Interactive challenges (click sequence, rotate, slider) need the
	// model to perform pointer actions. Return the challenge image with
	// specific instructions per type.
	if ch.Kind.InteractionAnswer() {
		return handleInteractiveCaptcha(ctx, sess, ch, params)
	}

	// Capture the challenge crop. For math without an image this is nil.
	// The caller may override the detected image selector via params.Selector.
	if ch.ImageSelector == "" && params.Selector != "" {
		ch.ImageSelector = params.Selector
	}
	var image []byte
	if ch.ImageSelector != "" {
		if img, capErr := captureCaptchaImage(ctx, sess, ch); capErr == nil {
			image = img
		} else if ch.Kind == captcha.KindImageText {
			return fantasy.NewTextErrorResponse("captcha image capture failed: " + capErr.Error()), nil
		}
	}

	// Try offline solvers first (math; remote token farm if configured).
	if captchaChain != nil && len(captchaChain.Solvers()) > 0 {
		if result, solveErr := captchaChain.Solve(ctx, ch, image); solveErr == nil && result.Solved && !result.Solution.IsEmpty() {
			report := CaptchaResult{
				Status:         "solved",
				Kind:           string(ch.Kind),
				Answer:         firstNonEmpty(result.Solution.Text, result.Solution.Token),
				Solver:         result.Solution.Solver,
				Confidence:     result.Solution.Confidence,
				SiteKey:        ch.SiteKey,
				PageURL:        ch.PageURL,
				Question:       ch.Question,
				AnswerSelector: ch.AnswerSelector,
				VerifySelector: ch.VerifySelector,
				Attempts:       result.Attempts,
				Fallbacks:      result.FallbackCount,
			}
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
