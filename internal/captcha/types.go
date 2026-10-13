// Package captcha detects and solves page CAPTCHA challenges so browser
// automation can finish sign-in and form flows without a human handoff.
//
// The package is deliberately free of browser and model types: it takes
// challenge images and page metadata in, and returns answers and tokens
// out. Capture, fill and tool plumbing live in the browser tool layer.
package captcha

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Kind classifies the challenge family. Solvers declare which kinds they
// accept so the chain never sends a reCAPTCHA token request to an OCR
// backend or an image to a token farm.
type Kind string

const (
	// KindImageText is a classic raster challenge: distorted characters
	// typed into a nearby text box. Solved as text.
	KindImageText Kind = "image_text"
	// KindMath is an arithmetic expression rendered as text or image.
	// Solved as a computed number.
	KindMath Kind = "math"
	// KindCheckbox is a bare "I'm not a robot" box with no widget
	// provider. A single click is usually enough.
	KindCheckbox Kind = "checkbox"
	// KindRecaptchaV2 is Google reCAPTCHA v2 (sitekey + optional image
	// grid). Solved as a response token.
	KindRecaptchaV2 Kind = "recaptcha_v2"
	// KindRecaptchaV3 is Google reCAPTCHA v3 (invisible score check).
	// Solved as a response token with an action label.
	KindRecaptchaV3 Kind = "recaptcha_v3"
	// KindHCaptcha is hCaptcha (sitekey + optional image grid).
	KindHCaptcha Kind = "hcaptcha"
	// KindTurnstile is Cloudflare Turnstile (sitekey, often invisible).
	KindTurnstile Kind = "turnstile"
	// KindClick requires clicking a sequence of icons/images in order.
	KindClick Kind = "click"
	// KindRotate requires rotating an image to its correct orientation.
	KindRotate Kind = "rotate"
	// KindSlider requires dragging a slider to fill a puzzle gap.
	KindSlider Kind = "slider"
	// KindKeyCaptcha is the KeyCAPTCHA widget (drag piece or arrow click).
	KindKeyCaptcha Kind = "keycaptcha"
	// KindUnknown is a detected widget the classifier could not name.
	KindUnknown Kind = "unknown"
)

// TextAnswer reports whether the solution is typed into a field rather
// than injected as a token.
func (k Kind) TextAnswer() bool {
	return k == KindImageText || k == KindMath
}

// TokenAnswer reports whether the solution is a provider response token.
func (k Kind) TokenAnswer() bool {
	return k == KindRecaptchaV2 || k == KindRecaptchaV3 || k == KindHCaptcha || k == KindTurnstile
}

// InteractionAnswer reports whether the solution requires pointer or
// keyboard interaction (click sequence, rotation, slider drag).
func (k Kind) InteractionAnswer() bool {
	return k == KindClick || k == KindRotate || k == KindSlider || k == KindKeyCaptcha
}

// Challenge is one detected widget on a page. It never carries pixels or
// secrets: only the metadata a solver needs and the DOM handles the
// browser layer uses to capture and fill.
type Challenge struct {
	Kind Kind `json:"kind"`
	// SiteKey is the widget public key for token providers.
	SiteKey string `json:"site_key,omitempty"`
	// Action is the reCAPTCHA v3 action label when the page supplies one.
	Action string `json:"action,omitempty"`
	// PageURL is the document URL the challenge sits on. Token farms
	// validate it; it is not a secret.
	PageURL string `json:"page_url"`
	// Question is the visible instruction text ("Güvenlik Resmi",
	// "Which images contain a bus", "5 + 3"). Not a secret.
	Question string `json:"question,omitempty"`
	// ImageSelector / AnswerSelector locate the challenge picture and the
	// answer box in the DOM. Token widgets leave both empty.
	ImageSelector  string `json:"image_selector,omitempty"`
	AnswerSelector string `json:"answer_selector,omitempty"`
	// VerifySelector is the button that submits the challenge, when one
	// is identifiable. Empty means the outer form is submitted instead.
	VerifySelector string `json:"verify_selector,omitempty"`
	// FrameID scopes capture and fill to the widget iframe when the
	// challenge lives inside one.
	FrameID string `json:"frame_id,omitempty"`
	// RotationHint is the current rotation in degrees for rotate
	// challenges (0 = original).
	RotationHint int `json:"rotation_hint,omitempty"`
	// ClickTargetSelector is the container holding the clickable icons
	// for click-sequence challenges.
	ClickTargetSelector string `json:"click_target_selector,omitempty"`
	// SliderSelector is the draggable handle for slider puzzles.
	SliderSelector string `json:"slider_selector,omitempty"`
	// Confidence is the detector's 0..1 belief in the classification.
	Confidence float64 `json:"confidence,omitempty"`
}

// Solution is one solver's answer. Text and Token are mutually
// exclusive and must match the challenge kind.
type Solution struct {
	// Text is the typed answer for image_text and math challenges.
	Text string `json:"text,omitempty"`
	// Token is the provider response token for widget challenges.
	Token string `json:"token,omitempty"`
	// Solver names the backend that produced the answer, for audit.
	Solver string `json:"solver"`
	// Duration is wall time spent solving.
	Duration time.Duration `json:"duration_ms"`
	// RawConfidence is the solver's 0..1 belief in the answer.
	Confidence float64 `json:"confidence,omitempty"`
}

// Attempt records one solver's try. The audit trail is kept for
// diagnostics and never contains challenge pixels.
type Attempt struct {
	Solver   string        `json:"solver"`
	Kind     Kind          `json:"kind"`
	OK       bool          `json:"ok"`
	Err      string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration_ms"`
}

// Result is the outcome of running the chain against one challenge.
type Result struct {
	Solution Solution  `json:"solution,omitempty"`
	Attempts []Attempt `json:"attempts"`
	// Solved is true when a usable answer exists.
	Solved bool `json:"solved"`
	// FallbackCount is how many solvers failed before the winner.
	FallbackCount int `json:"fallback_count,omitempty"`
}

// Solver turns a challenge and its captured pixels into an answer.
// Implementations must not log or persist the image.
type Solver interface {
	// Name identifies the solver in audit records.
	Name() string
	// Supports reports whether this solver can handle the kind.
	Supports(kind Kind) bool
	// Solve answers the challenge. Image is nil for token kinds that
	// only need site metadata.
	Solve(ctx context.Context, ch Challenge, image []byte) (Solution, error)
}

// ErrNoSolver means no configured solver supports the challenge kind.
var ErrNoSolver = errors.New("captcha: no configured solver supports this challenge kind")

// ErrUnsolved means every solver failed or produced an empty answer.
var ErrUnsolved = errors.New("captcha: challenge unsolved")

// ErrUnsupportedKind means the challenge kind is not solvable by the
// current chain (for example a checkbox with no clicker wired).
var ErrUnsupportedKind = errors.New("captcha: unsupported challenge kind")

// SolverError annotates a solver failure with its backend name so the
// chain can record a useful attempt row.
type SolverError struct {
	Solver string
	Err    error
}

func (e *SolverError) Error() string {
	return fmt.Sprintf("captcha solver %s: %v", e.Solver, e.Err)
}

func (e *SolverError) Unwrap() error { return e.Err }

// IsEmpty reports whether a solution carries no usable answer.
func (s Solution) IsEmpty() bool {
	return s.Text == "" && s.Token == ""
}
