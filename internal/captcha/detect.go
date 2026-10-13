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
