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
