package activity

import (
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The control island is the banner's interactive form. Everything here is
// presentation: a finished motion or a local selection never resolves a
// request; only Manager.Respond does, through the request's own service.

// islandRect is a surface anchored at its top center, in physical pixels.
type islandRect struct{ CX, Top, W, H, R float64 }

func lerpRect(a, b islandRect, t float64) islandRect {
	l := func(x, y float64) float64 { return x + (y-x)*t }
	return islandRect{l(a.CX, b.CX), l(a.Top, b.Top), l(a.W, b.W), l(a.H, b.H), l(a.R, b.R)}
}

const (
	islandOpen         = 340 * time.Millisecond
	islandClose        = 280 * time.Millisecond
	islandReducedFade  = 140 * time.Millisecond
	islandInputGuard   = 350 * time.Millisecond
	islandOpenOmega    = 17.0
	islandCloseOmega   = 21.0
	islandSpringDamped = 0.88
)

// islandMotion is a time-based, critically damped transition. Retargeting
// starts from the current visual geometry, so interruptions never jump.
type islandMotion struct {
	from, to islandRect
	start    time.Time
	duration time.Duration
	omega    float64
	reduced  bool
	ready    bool
}

// spring is the unit step response of a damped spring; with this damping
// it overshoots by about 0.3%, which reads as settled rather than bouncy.
func spring(omega, t float64) float64 {
	if t <= 0 {
		return 0
	}
	zeta := islandSpringDamped
	wd := omega * math.Sqrt(1-zeta*zeta)
	return 1 - math.Exp(-zeta*omega*t)*(math.Cos(wd*t)+zeta*omega/wd*math.Sin(wd*t))
}

func (m *islandMotion) at(now time.Time) (islandRect, bool) {
	if !m.ready {
		return m.to, true
	}
	elapsed := now.Sub(m.start)
	if elapsed >= m.duration {
		return m.to, true
	}
	if m.reduced {
		// Reduced motion swaps geometry at once; content cross-fades.
		return m.to, false
	}
	return lerpRect(m.from, m.to, spring(m.omega, elapsed.Seconds())), false
}

// retarget moves toward to from wherever the surface is now.
func (m *islandMotion) retarget(to islandRect, now time.Time, opening, reduced bool) {
	current, _ := m.at(now)
	if !m.ready {
		current = to
	}
	m.from, m.to, m.start, m.reduced, m.ready = current, to, now, reduced, true
	m.duration, m.omega = islandClose, islandCloseOmega
	if opening {
		m.duration, m.omega = islandOpen, islandOpenOmega
	}
	if reduced {
		m.duration = islandReducedFade
	}
}

// snap places the surface without motion, e.g. after a monitor change.
func (m *islandMotion) snap(to islandRect) {
	m.from, m.to, m.ready, m.duration = to, to, true, 0
}

func smoothstep(a, b, x float64) float64 {
	t := max(0, min(1, (x-a)/(b-a)))
	return t * t * (3 - 2*t)
}

// islandOpacities orders content around the shape: compact content leaves
// early, expanded content arrives once the surface has room for it.
func islandOpacities(progress float64) (compact, expanded float64) {
	return 1 - smoothstep(0, .3, progress), smoothstep(.5, 1, progress)
}

// islandProgress is how far a surface is between its two shapes.
func islandProgress(current, compact, expanded islandRect) float64 {
	span := expanded.H - compact.H
	if math.Abs(span) < 1 {
		return 1
	}
	return max(0, min(1, (current.H-compact.H)/span))
}

// islandControlKind is one focusable element of the expanded island.
type islandControlKind uint8

const (
	controlChoice islandControlKind = iota + 1
	controlText
	controlBack
	controlNext
	controlSubmit
	controlDecision
	controlStop
)

type islandControl struct {
	kind     islandControlKind
	choice   string
	decision PermissionDecision
}

// islandKey is a keyboard command, independent of the platform's codes.
type islandKey uint8

const (
	keyTab islandKey = iota + 1
	keyShiftTab
	keyUp
	keyDown
	keyLeft
	keyRight
	keyHome
	keyEnd
	keySpace
	keyEnter
	keyShiftEnter
	keyBackspace
	keyDelete
	keyPageUp
	keyPageDown
)

// islandForm is the local, unsent state of one displayed request.
type islandForm struct {
	prompt    *Prompt
	page      int
	selected  []map[string]bool
	yes       []*bool
	text      [][]rune
	caret     []int
	focus     int
	submitted bool
	// opened guards against keys that were already in flight when the
	// island appeared, such as an Enter meant for another application.
	opened time.Time
}

func newIslandForm(p *Prompt, now time.Time) *islandForm {
	f := &islandForm{prompt: p, opened: now, focus: -1}
	n := len(p.Questions)
	f.selected, f.yes, f.text, f.caret = make([]map[string]bool, n), make([]*bool, n), make([][]rune, n), make([]int, n)
	for i := range f.selected {
		f.selected[i] = map[string]bool{}
	}
	if p.Kind == KindQuestion {
		// Questions focus their first option; permissions focus nothing,
		// so a stray Enter cannot approve anything.
		f.focus = 0
	}
	return f
}

func (f *islandForm) question() *PromptQuestion {
	if f.prompt.Kind != KindQuestion || f.page >= len(f.prompt.Questions) {
		return nil
	}
	return &f.prompt.Questions[f.page]
}

func (f *islandForm) last() bool { return f.page >= len(f.prompt.Questions)-1 }

// choices are the selectable options of the current question; yes/no is
// presented as two options.
func (f *islandForm) choices() []PromptChoice {
	q := f.question()
	if q == nil {
		return nil
	}
	if q.Type == QuestionYesNo {
		return []PromptChoice{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}}
	}
	return q.Choices
}

// controls lists focusable elements in visual order.
func (f *islandForm) controls() []islandControl {
	var out []islandControl
	if f.prompt.Kind == KindPermission {
		for _, d := range []PermissionDecision{DecisionDeny, DecisionAllowSession, DecisionAllowOnce} {
			for _, supported := range f.prompt.Permission.Decisions {
				if supported == d {
					out = append(out, islandControl{kind: controlDecision, decision: d})
				}
			}
		}
		return out
	}
	q := f.question()
	if q == nil {
		return nil
	}
	for _, c := range f.choices() {
		out = append(out, islandControl{kind: controlChoice, choice: c.ID})
	}
	if q.Type == QuestionFreeText {
		out = append(out, islandControl{kind: controlText})
	}
	if f.page > 0 {
		out = append(out, islandControl{kind: controlBack})
	}
	if f.last() {
		out = append(out, islandControl{kind: controlSubmit})
	} else {
		out = append(out, islandControl{kind: controlNext})
	}
	return out
}

func (f *islandForm) focused() (islandControl, bool) {
	controls := f.controls()
	if f.focus < 0 || f.focus >= len(controls) {
		return islandControl{}, false
	}
	return controls[f.focus], true
}

// answered reports whether the current question has an answer to send.
func (f *islandForm) answered(page int) bool {
	q := f.prompt.Questions[page]
	switch q.Type {
	case QuestionYesNo:
		return f.yes[page] != nil
	case QuestionFreeText:
		return strings.TrimSpace(string(f.text[page])) != ""
	default:
		return len(f.selected[page]) > 0
	}
}

// enabled reports whether a control can act now.
func (f *islandForm) enabled(c islandControl) bool {
	if f.submitted {
		return c.kind == controlStop
	}
	switch c.kind {
	case controlNext:
		return f.answered(f.page)
	case controlSubmit:
		for i := range f.prompt.Questions {
			if !f.answered(i) {
				return false
			}
		}
		return true
	}
	return true
}

func (f *islandForm) isSelected(id string) bool {
	q := f.question()
	if q == nil {
		return false
	}
	if q.Type == QuestionYesNo {
		return f.yes[f.page] != nil && *f.yes[f.page] == (id == "yes")
	}
	return f.selected[f.page][id]
}

// choose changes only local selection; it never sends anything.
func (f *islandForm) choose(id string) {
	q := f.question()
	if q == nil || f.submitted {
		return
	}
	switch q.Type {
	case QuestionYesNo:
		yes := id == "yes"
		f.yes[f.page] = &yes
	case QuestionMultiChoice:
		if f.selected[f.page][id] {
			delete(f.selected[f.page], id)
		} else {
			f.selected[f.page][id] = true
		}
	default:
		f.selected[f.page] = map[string]bool{id: true}
	}
}

// focusPrimary moves focus to Next or Submit after a selection.
func (f *islandForm) focusPrimary() {
	for i, c := range f.controls() {
		if c.kind == controlNext || c.kind == controlSubmit {
			f.focus = i
		}
	}
}

// response builds the payload for Manager.Respond.
func (f *islandForm) response(decision PermissionDecision) PromptResponse {
	if f.prompt.Kind == KindPermission {
		return PromptResponse{Decision: decision}
	}
	answers := make([]PromptAnswer, len(f.prompt.Questions))
	for i, q := range f.prompt.Questions {
		a := PromptAnswer{QuestionID: q.ID}
		switch q.Type {
		case QuestionYesNo:
			a.Yes = f.yes[i]
		case QuestionFreeText:
			a.Text = strings.TrimSpace(string(f.text[i]))
		default:
			for _, c := range q.Choices {
				if f.selected[i][c.ID] {
					a.Selected = append(a.Selected, c.ID)
				}
			}
		}
		answers[i] = a
	}
	return PromptResponse{Answers: answers}
}

// activate performs a control. It returns a response exactly once per
// form; later activations are ignored until the request changes.
func (f *islandForm) activate(c islandControl) (PromptResponse, bool) {
	if !f.enabled(c) {
		return PromptResponse{}, false
	}
	switch c.kind {
	case controlChoice:
		f.choose(c.choice)
		if q := f.question(); q != nil && q.Type != QuestionMultiChoice {
			f.focusPrimary()
		}
	case controlBack:
		if f.page > 0 {
			f.page--
			f.focus = 0
		}
	case controlNext:
		f.page++
		f.focus = 0
	case controlSubmit, controlDecision:
		f.submitted = true
		return f.response(c.decision), true
	}
	return PromptResponse{}, false
}

// key applies one keyboard command at now.
func (f *islandForm) key(k islandKey, now time.Time) (PromptResponse, bool) {
	if now.Sub(f.opened) < islandInputGuard {
		return PromptResponse{}, false
	}
	controls := f.controls()
	current, hasFocus := f.focused()
	if hasFocus && current.kind == controlText && f.editKey(k) {
		return PromptResponse{}, false
	}
	move := func(delta int) {
		if len(controls) == 0 {
			return
		}
		if f.focus < 0 {
			if delta > 0 {
				f.focus = 0
			} else {
				f.focus = len(controls) - 1
			}
			return
		}
		f.focus = (f.focus + delta + len(controls)) % len(controls)
	}
	switch k {
	case keyTab:
		move(1)
	case keyShiftTab:
		move(-1)
	case keyDown, keyRight:
		move(1)
	case keyUp, keyLeft:
		move(-1)
	case keyHome:
		f.focus = 0
	case keyEnd:
		f.focus = len(controls) - 1
	case keySpace, keyEnter:
		if !hasFocus {
			return PromptResponse{}, false
		}
		if k == keySpace && current.kind != controlChoice {
			// Space toggles options; buttons need Enter or a click.
			return PromptResponse{}, false
		}
		return f.activate(current)
	}
	return PromptResponse{}, false
}

// editKey handles caret keys inside the text field.
func (f *islandForm) editKey(k islandKey) bool {
	text, caret := f.text[f.page], f.caret[f.page]
	switch k {
	case keyLeft:
		f.caret[f.page] = max(0, caret-1)
	case keyRight:
		f.caret[f.page] = min(len(text), caret+1)
	case keyHome:
		f.caret[f.page] = 0
	case keyEnd:
		f.caret[f.page] = len(text)
	case keyBackspace:
		if caret > 0 && !f.submitted {
			f.text[f.page] = append(text[:caret-1:caret-1], text[caret:]...)
			f.caret[f.page] = caret - 1
		}
	case keyDelete:
		if caret < len(text) && !f.submitted {
			f.text[f.page] = append(text[:caret:caret], text[caret+1:]...)
		}
	case keyShiftEnter:
		f.insert("\n")
	case keyEnter:
		// Enter leaves the field for the primary button; a second Enter
		// sends, so typing never submits by accident.
		f.focusPrimary()
	default:
		return false
	}
	return true
}

// insert adds typed, pasted or IME-composed text at the caret.
func (f *islandForm) insert(s string) {
	if f.submitted || f.question() == nil || f.question().Type != QuestionFreeText {
		return
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var add []rune
	for _, r := range s {
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\t') {
			continue
		}
		if r == '\t' {
			r = ' '
		}
		add = append(add, r)
	}
	text, caret := f.text[f.page], f.caret[f.page]
	add = add[:min(len(add), max(0, MaxPromptText-len(text)))]
	next := make([]rune, 0, len(text)+len(add))
	next = append(append(append(next, text[:caret]...), add...), text[caret:]...)
	f.text[f.page], f.caret[f.page] = next, caret+len(add)
}
