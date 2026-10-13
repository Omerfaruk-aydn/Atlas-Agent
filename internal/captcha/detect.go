package captcha

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DetectScript classifies visible verification widgets on a page. It runs
// in the page world (not the isolated secret world) because it must read
// cross-origin iframe URLs and widget attributes. It never reads input
// values or cookies.
const DetectScript = `(() => {
const vis = e => { try { return e.getClientRects().length > 0 && getComputedStyle(e).visibility !== "hidden" && getComputedStyle(e).display !== "none"; } catch { return false; } };
const text = (document.body && document.body.innerText || "").slice(0, 8000);
const lower = text.toLowerCase();
const has = (...w) => w.some(x => lower.includes(x));
const rect = e => { try { const r = e.getBoundingClientRect(); return {x: r.x + scrollX, y: r.y + scrollY, w: r.width, h: r.height}; } catch { return null; } };
const sel = e => {
  if (!e || e.nodeType !== 1) return "";
  if (e.id) { const s = "#" + CSS.escape(e.id); if (document.querySelectorAll(s).length === 1) return s; }
  if (e.name && e.tagName === "INPUT") { const s = e.tagName.toLowerCase() + "[name=" + JSON.stringify(e.name) + "]"; if (document.querySelectorAll(s).length === 1) return s; }
  if (e.tagName === "IMG" && e.src) { const u = e.src.split("/").pop().split("?")[0]; if (u && u.length < 80) { const s = 'img[src*="' + CSS.escape(u) + '"]'; if (document.querySelectorAll(s).length === 1) return s; } }
  const path = [];
  for (let n = e; n && n.nodeType === 1 && path.length < 5; n = n.parentElement) {
    let p = n.tagName.toLowerCase();
    if (n.id) { p += "#" + CSS.escape(n.id); path.unshift(p); break; }
    const i = n.parentElement ? Array.prototype.indexOf.call(n.parentElement.children, n) + 1 : 1;
    path.unshift(p + ":nth-child(" + i + ")");
  }
  return path.join(" > ");
};

const out = {kind: "none", site_key: "", action: "", question: "", image_selector: "", answer_selector: "", verify_selector: "", frame_id: "", confidence: 0, rect: null, page_url: location.href};

const recaptchaIframe = document.querySelector('iframe[src*="recaptcha"], .g-recaptcha iframe, iframe[src*="google.com/recaptcha"]');
const hcaptchaIframe = document.querySelector('iframe[src*="hcaptcha"], .h-captcha iframe');
const turnstileIframe = document.querySelector('iframe[src*="challenges.cloudflare.com"], .cf-turnstile iframe, #turnstile-wrapper iframe');

const siteKeyOf = (root) => {
  const host = root && root.closest && (root.closest("[data-sitekey]") || root.closest(".g-recaptcha,.h-captcha,.cf-turnstile,#turnstile-wrapper,[data-sitekey]"));
  const el = host || document.querySelector("[data-sitekey]");
  return (el && (el.getAttribute("data-sitekey") || el.getAttribute("data-sitekey".toLowerCase()))) || "";
};

if (recaptchaIframe && vis(recaptchaIframe)) {
  const src = recaptchaIframe.getAttribute("src") || "";
  out.kind = src.includes("anchor") || src.includes("bframe") ? "recaptcha_v2" : "recaptcha_v2";
  out.site_key = siteKeyOf(recaptchaIframe);
  out.frame_id = "";
  out.confidence = 0.9;
  out.rect = rect(recaptchaIframe);
  const actionEl = document.querySelector("[data-action], .g-recaptcha[data-action]");
  if (actionEl) out.action = actionEl.getAttribute("data-action") || "";
  if (lower.includes("invisible") || src.includes("size=invisible")) out.kind = "recaptcha_v3";
  return out;
}
if (hcaptchaIframe && vis(hcaptchaIframe)) {
  out.kind = "hcaptcha";
  out.site_key = siteKeyOf(hcaptchaIframe);
  out.confidence = 0.9;
  out.rect = rect(hcaptchaIframe);
  return out;
}
if (turnstileIframe && vis(turnstileIframe)) {
  out.kind = "turnstile";
  out.site_key = siteKeyOf(turnstileIframe);
  out.confidence = 0.9;
  out.rect = rect(turnstileIframe);
  return out;
}

const checkboxes = [...document.querySelectorAll('input[type="checkbox"]')].filter(vis);
const robotBox = checkboxes.find(e => {
  const label = ((e.getAttribute("aria-label") || "") + " " + (e.closest("label") && e.closest("label").innerText || "")).toLowerCase();
  return label.includes("robot") || label.includes("robot değilim") || label.includes("not a robot");
});
if (robotBox && !recaptchaIframe && !hcaptchaIframe && !turnstileIframe) {
  out.kind = "checkbox";
  out.answer_selector = sel(robotBox);
  out.confidence = 0.85;
  out.rect = rect(robotBox);
  return out;
}

const captchaWords = ["güvenlik resmi", "guvenlik resmi", "security image", "security picture", "doğrulama kodu", "verification code", "captcha", "güvenlik kodu", "guvenlik kodu", "emniyet kodu"];
const mathWord = lower.includes("hesaplayın") || lower.includes("hesapla") || lower.includes("solve") || lower.match(/\d+\s*[+\-x×÷*/]\s*\d+/);
const questionWord = captchaWords.some(w => lower.includes(w));

// Detect click-sequence, rotate and slider challenge widgets before the
// generic image-text path so the right interaction type is reported.
const clickWords = lower.includes("select in this order") || lower.includes("tıkla") || lower.includes("click the") || lower.includes("sırayla") || lower.includes("sirayla") || lower.includes("doğru sırayla") || lower.includes("dogru sirayla") || lower.includes("click in order") || lower.includes("seçin") || lower.includes("secin");
const rotateWords = lower.includes("döndür") || lower.includes("dondur") || lower.includes("rotate") || lower.includes("turn the") || lower.includes("correct orientation") || lower.includes("doğru yön") || lower.includes("dogru yon") || lower.includes("çevir") || lower.includes("cevir");
const sliderWords = lower.includes("slide") || lower.includes("sürükle") || lower.includes("surukle") || lower.includes("drag") || lower.includes("puzzle") || lower.includes("yapboz") || lower.includes("kaydır") || lower.includes("kaydir") || lower.includes("slide to complete") || lower.includes("tamamla");
// GeeTest: detect by widget class presence even before it fully loads.
// The radar/click-to-verify state also implies a slider puzzle underneath.
const geetestEl = document.querySelector('.geetest_slider_button, .geetest_canvas_slice, .geetest_canvas_bg, .geetest_widget, .geetest_radar_tip, .geetest_radar, [class*="geetest_canvas"], [class*="geetest_slider"], [class*="geetest"]');
const geetestActive = document.querySelector('.geetest_canvas_slice, .geetest_canvas_bg, .geetest_slider_button');
const rotateEl = document.querySelector('img[class*="rotate"], [class*="rotate-captcha"], [class*="image-rotate"], canvas[class*="rotate"], [class*="rotate_btn"], [class*="rotate-btn"], [class*="rotating"]');
const rotateBtns = document.querySelectorAll('[class*="rotate_left"], [class*="rotate_right"], [class*="rotate-left"], [class*="rotate-right"], button[class*="rotate"]');
const hasRotateBtns = rotateBtns.length >= 2;
const clickCaptchaEl = document.querySelector('[class*="click-captcha"], [class*="clickcaptcha"], [class*="icon-captcha"], [class*="clicky"], [class*="click_verify"], [class*="keycaptcha_widget"], img[class*="click"]');

// KeyCAPTCHA has its own widget container that may render empty; detect
// it explicitly so it is not misclassified as rotate or click.
const keycaptchaEl = document.querySelector('[class*="keycaptcha"], [id*="keycaptcha"], iframe[src*="keycaptcha"]');
if (keycaptchaEl) {
  out.kind = "keycaptcha";
  out.confidence = 0.85;
  out.rect = rect(keycaptchaEl);
  out.question = "KeyCAPTCHA widget: drag the piece or click the arrow";
  return out;
}
// GeeTest detection: always classify as slider when any GeeTest widget is
// present. Even the initial "Click to verify" radar state leads to a slider
// puzzle after the user clicks it.
if (geetestEl) {
  out.kind = "slider";
  out.slider_selector = sel(geetestActive || geetestEl);
  out.confidence = geetestActive ? 0.95 : 0.85;
  out.rect = rect(geetestActive || geetestEl);
  out.question = "GeeTest slider puzzle: click the widget to open it, then drag the slider handle to fill the gap";
  // Also grab the puzzle background image selector for gap detection.
  const bg = document.querySelector('.geetest_canvas_bg, canvas[class*="geetest_canvas_bg"], .geetest_canvas_slice');
  if (bg) out.image_selector = sel(bg);
  return out;
}
if ((rotateEl || hasRotateBtns) && (rotateWords || hasRotateBtns || document.querySelector('[class*="rotate"]'))) {
  out.kind = "rotate";
  out.image_selector = sel(rotateEl);
  out.slider_selector = sel(rotateEl || rotateBtns[0]);
  out.confidence = 0.9;
  out.rect = rect(rotateEl || rotateBtns[0]);
  out.question = "rotate the image to its correct upright orientation";
  return out;
}
if (clickCaptchaEl && (clickWords || document.querySelector('[class*="clickcaptcha"], [class*="click-captcha"]'))) {
  out.kind = "click";
  out.image_selector = sel(clickCaptchaEl);
  out.click_target_selector = sel(clickCaptchaEl);
  out.confidence = 0.85;
  out.rect = rect(clickCaptchaEl);
  out.question = "click the icons in the correct order";
  return out;
}
// Generic text-driven classification when no specific widget element is
// found but the page clearly describes one of these challenge types.
if (!geetestEl && !rotateEl && !clickCaptchaEl) {
  if (clickWords && questionWord) {
    out.kind = "click";
    out.confidence = 0.7;
    out.question = "click the icons in the correct order";
    return out;
  }
  if (rotateWords && questionWord) {
    out.kind = "rotate";
    out.confidence = 0.7;
    out.question = "rotate the image to its correct orientation";
    return out;
  }
  if (sliderWords && questionWord) {
    out.kind = "slider";
    out.confidence = 0.7;
    out.question = "drag the slider to fill the puzzle gap";
    return out;
  }
}

const imgs = [...document.querySelectorAll("img")].filter(vis).map(e => ({
  e, src: (e.getAttribute("src") || "").toLowerCase(), alt: (e.getAttribute("alt") || "").toLowerCase(),
  cls: (e.className || "").toString().toLowerCase()
}));
// Prefer images that look like challenge rasters (auth/captcha/verify/
// security in src/alt/class) and are not decorative icons (small squares
// like graduationcap.png). A challenge image is typically wide and short.
const captchaImg = imgs.find(i => {
  const looksChallenge = i.src.includes("captcha") || i.src.includes("verify") || i.src.includes("security") ||
    i.src.includes("challenge") || i.src.includes("auth") || i.src.includes("code") ||
    i.alt.includes("captcha") || i.alt.includes("güvenlik") || i.alt.includes("guvenlik") || i.alt.includes("security") ||
    i.cls.includes("captcha") || i.cls.includes("challenge") || i.cls.includes("auth");
  const decorative = i.src.includes("graduation") || i.src.includes("logo") || i.src.includes("icon") ||
    i.src.includes("banner") || i.src.includes("header") || i.alt === "img";
  if (decorative) return false;
  if (looksChallenge) return true;
  // Fallback: a mid-size image inside a form that sits near a captcha-like
  // input, with wide/short aspect (challenge rasters are landscape).
  const near = i.e.closest("form") && i.e.naturalWidth > 50 && i.e.naturalHeight > 15 && i.e.naturalWidth < 600;
  const aspect = i.e.naturalWidth / Math.max(i.e.naturalHeight, 1);
  return near && aspect > 1.5 && aspect < 12 && (questionWord || mathWord);
});

const allInputs = [...document.querySelectorAll("input,textarea")].filter(vis);
// A CAPTCHA answer box is a short text input near the challenge image.
// Never pick username/password/login fields. Prefer explicit captcha
// naming, then spatial proximity to the challenge image.
const excludeInput = e => {
  const id = (e.getAttribute("id") || "").toLowerCase();
  const name = (e.getAttribute("name") || "").toLowerCase();
  const ac = (e.getAttribute("autocomplete") || "").toLowerCase();
  return id.includes("user") || id.includes("kullanici") || id.includes("login") || id.includes("eposta") ||
    id.includes("mail") || id.includes("password") || id.includes("parola") || id.includes("sifre") ||
    name.includes("user") || name.includes("kullanici") || name.includes("login") || name.includes("eposta") ||
    name.includes("mail") || name.includes("password") || name.includes("parola") || name.includes("sifre") ||
    ac.includes("user") || ac.includes("email") || ac.includes("password");
};
// Pass 1: explicit captcha/code/security naming (and not a login field).
const answerInput = allInputs.find(e => {
  if (e.type === "password" || e.type === "hidden" || e.type === "submit" || e.type === "checkbox") return false;
  if (excludeInput(e)) return false;
  const id = (e.getAttribute("id") || "").toLowerCase();
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
