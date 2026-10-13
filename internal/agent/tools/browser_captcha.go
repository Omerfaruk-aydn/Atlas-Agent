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
