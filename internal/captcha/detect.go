  const name = (e.getAttribute("name") || "").toLowerCase();
  return id.includes("captcha") || name.includes("captcha") || id.includes("guvenlik") || name.includes("guvenlik") ||
    id.includes("code") || name.includes("code") || id.includes("verify") || name.includes("verify") ||
    id.includes("security") || name.includes("security") || id.includes("resim") || name.includes("resim");
});
// Pass 2: an empty text input near the challenge image (within 200px
// vertically), excluding login fields.
let answerNear = null;
if (!answerInput && captchaImg) {
  const imgRect = captchaImg.e.getBoundingClientRect();
  answerNear = allInputs.find(e => {
    if (e.type !== "text" && e.type !== "") return false;
    if (excludeInput(e)) return false;
    if (e.value && e.value.length > 0) return false;
    const r = e.getBoundingClientRect();
    const dy = Math.abs(r.top - imgRect.bottom);
    return dy < 200 && r.width > 30 && r.width < 300;
  });
}
// Pass 3: any empty short text input on the page (last resort), still
// excluding login fields.
const answerFallback = !answerInput && !answerNear ? allInputs.find(e => {
  if (e.type !== "text" && e.type !== "") return false;
  if (excludeInput(e)) return false;
  return !e.value || e.value.length === 0;
}) : null;
const finalAnswer = answerInput || answerNear || answerFallback;

const verifyBtn = [...document.querySelectorAll('button, input[type="submit"], input[type="button"], a[role="button"]')].filter(vis).find(b => {
  const t = ((b.innerText || b.value || "") + " " + (b.getAttribute("aria-label") || "")).toLowerCase().trim();
  return t.includes("giriş") || t.includes("giris") || t.includes("login") || t.includes("sign in") ||
    t.includes("doğrula") || t.includes("verify") || t.includes("onayla") || t.includes("confirm") ||
    t.includes("kontrol") || t.includes("check") || t.includes("submit") || t.includes("gönder") || t.includes("devam");
});

if (captchaImg || (questionWord && finalAnswer)) {
  const isMath = !!mathWord && !captchaImg;
  out.kind = isMath ? "math" : "image_text";
  out.image_selector = captchaImg ? sel(captchaImg.e) : "";
  out.answer_selector = finalAnswer ? sel(finalAnswer) : "";
  out.verify_selector = verifyBtn ? sel(verifyBtn) : "";
  out.confidence = captchaImg && finalAnswer ? 0.95 : captchaImg || finalAnswer ? 0.75 : 0.6;
  out.rect = captchaImg ? rect(captchaImg.e) : (finalAnswer ? rect(finalAnswer) : null);
  if (isMath) {
    const m = lower.match(/(\d+\s*[+\-x×÷*/]\s*\d+(?:\s*[+\-x×÷*/]\s*\d+)*)/);
    if (m) out.question = m[1];
  }
  if (!out.question && captchaImg) out.question = (captchaImg.alt || captchaImg.e.getAttribute("title") || "security image");
  return out;
}

if (questionWord && !finalAnswer) {
  out.kind = "image_text";
  out.question = "captcha widget detected without a clear answer field";
  out.confidence = 0.5;
  return out;
}

if (has("i'm not a robot", "ben robot değilim", "ben robot degilim")) {
  out.kind = "checkbox";
  out.confidence = 0.6;
}
return out;
})()`

// Detected is the decoded DetectScript result plus the raw page URL when
// the script did not run.
type Detected struct {
	Kind           Kind     `json:"kind"`
	SiteKey        string   `json:"site_key"`
	Action         string   `json:"action"`
	Question       string   `json:"question"`
	ImageSelector  string   `json:"image_selector"`
	AnswerSelector string   `json:"answer_selector"`
	VerifySelector string   `json:"verify_selector"`
	FrameID        string   `json:"frame_id"`
	RotationHint   int      `json:"rotation_hint"`
	ClickTarget    string   `json:"click_target_selector"`
	SliderSelector string   `json:"slider_selector"`
	Confidence     float64  `json:"confidence"`
	Rect           *DOMRect `json:"rect"`
	PageURL        string   `json:"page_url"`
}

// DOMRect is an element bounding box in CSS page coordinates.
type DOMRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// ParseDetect turns the JSON string returned by DetectScript into a
// Detected value and then a Challenge.
func ParseDetect(raw string, pageURL string) (Detected, Challenge, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Detected{}, Challenge{}, fmt.Errorf("captcha: empty detection result")
	}
	// Eval may wrap the value in quotes when the driver returns a JSON
	// string; accept both shapes.
	var d Detected
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		var s string
		if err2 := json.Unmarshal([]byte(raw), &s); err2 == nil {
			if err3 := json.Unmarshal([]byte(s), &d); err3 == nil {
				goto decoded
			}
		}
		return Detected{}, Challenge{}, fmt.Errorf("captcha: decode detection: %w", err)
	}
decoded:
	if d.PageURL == "" {
		d.PageURL = pageURL
	}
	if d.Kind == "" {
		d.Kind = KindUnknown
	}
	// Normalize aliases the script may emit.
	switch strings.ToLower(string(d.Kind)) {
	case "recaptcha", "g-recaptcha", "recaptcha_v2":
		d.Kind = KindRecaptchaV2
	case "recaptcha_v3", "recaptcha_v3_invisible":
		d.Kind = KindRecaptchaV3
	case "hcaptcha", "h-captcha":
		d.Kind = KindHCaptcha
	case "turnstile", "cf-turnstile":
		d.Kind = KindTurnstile
	case "image", "img", "text", "image_captcha":
		d.Kind = KindImageText
	case "arithmetic", "math_captcha":
		d.Kind = KindMath
	case "none", "":
		d.Kind = KindUnknown
	}
	ch := Challenge{
		Kind:                d.Kind,
		SiteKey:             d.SiteKey,
		Action:              d.Action,
		PageURL:             d.PageURL,
		Question:            strings.TrimSpace(d.Question),
		ImageSelector:       d.ImageSelector,
		AnswerSelector:      d.AnswerSelector,
		VerifySelector:      d.VerifySelector,
		FrameID:             d.FrameID,
		RotationHint:        d.RotationHint,
		ClickTargetSelector: d.ClickTarget,
		SliderSelector:      d.SliderSelector,
		Confidence:          d.Confidence,
	}
	return d, ch, nil
}

// Present reports whether the detector found something worth solving.
func (d Detected) Present() bool {
	return d.Kind != KindUnknown && d.Kind != KindNone && d.Kind != ""
}

// KindNone is the script's sentinel for "no challenge".
const KindNone Kind = "none"
