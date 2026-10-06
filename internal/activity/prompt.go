package activity

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// PromptKind distinguishes the two blocking user decisions.
type PromptKind uint8

const (
	// KindQuestion is a question tool batch.
	KindQuestion PromptKind = iota + 1
	// KindPermission is a permission service request.
	KindPermission
)

// PermissionDecision is a decision the permission service supports.
type PermissionDecision uint8

const (
	// DecisionNone is not a decision.
	DecisionNone PermissionDecision = iota
	// DecisionAllowOnce grants this request only.
	DecisionAllowOnce
	// DecisionAllowSession grants matching requests for this session.
	DecisionAllowSession
	// DecisionDeny refuses this request.
	DecisionDeny
)

// Question types mirror the question service.
const (
	QuestionYesNo        = "yes_no"
	QuestionSingleChoice = "single_choice"
	QuestionMultiChoice  = "multi_choice"
	QuestionFreeText     = "free_text"
)

// MaxPromptText bounds a free-text answer, in runes.
const MaxPromptText = 4000

// PromptChoice is one selectable option.
type PromptChoice struct {
	ID, Label, Description string
}

// PromptQuestion is one question of a batch.
type PromptQuestion struct {
	ID, Type, Label, Text, Description string
	Choices                            []PromptChoice
}

// PromptPermission is the authorized, redacted description of a request.
// It is built from the requesting tool's own fields, never from page or
// model prose presented as a decision.
type PromptPermission struct {
	Tool, Action, Target, Detail, Scope string
	// Hidden marks content withheld from display, such as typed text.
	Hidden    bool
	Decisions []PermissionDecision
}

// Prompt is an immutable snapshot of one pending request.
type Prompt struct {
	Kind     PromptKind
	ID       string
	Session  string
	Revision uint64
	// Queued counts pending requests behind this one in the same run.
	Queued                           int
	Questions                        []PromptQuestion
	ConfirmTitle, ConfirmDescription string
	Permission                       PromptPermission
	Since                            time.Time
}

// PromptAnswer answers one question.
type PromptAnswer struct {
	QuestionID string
	Selected   []string
	Text       string
	Yes        *bool
}

// PromptResponse is a submitted decision.
type PromptResponse struct {
	Decision PermissionDecision
	Answers  []PromptAnswer
}

// PromptResponder forwards a response to the request's own service. It
// returns true only when that call resolved the still-pending request.
type PromptResponder func(PromptResponse) bool

// PromptOutcome is how a pending request ended.
type PromptOutcome uint8

const (
	// OutcomeAnswered is an answered question.
	OutcomeAnswered PromptOutcome = iota + 1
	// OutcomeGranted is a granted permission.
	OutcomeGranted
	// OutcomeDenied is a refused permission.
	OutcomeDenied
	// OutcomeCancelled is a withdrawn or cancelled request.
	OutcomeCancelled
)

// Errors returned for a response that must not reach a service.
var (
	ErrPromptStale   = errors.New("prompt is no longer pending")
	ErrPromptBusy    = errors.New("prompt response is already being delivered")
	ErrPromptInvalid = errors.New("prompt response is invalid")
)

type pendingPrompt struct {
	prompt     Prompt
	respond    PromptResponder
	delivering bool
}

var (
	promptRevision atomic.Uint64
	promptMu       sync.Mutex
	promptWake     = make(chan struct{})
	promptFlows    = map[*flow]struct{}{}
)

// promptChanged wakes every waiter so it can re-evaluate its gate.
func promptChanged() {
	promptMu.Lock()
	close(promptWake)
	promptWake = make(chan struct{})
	promptMu.Unlock()
}

// PendingPrompt is a registered request; Done must run when it resolves.
type PendingPrompt struct {
	f    *flow
	p    *pendingPrompt
	once sync.Once
}

// Revision is the request's ownership revision; zero when not displayed.
func (h *PendingPrompt) Revision() uint64 {
	if h == nil || h.p == nil {
		return 0
	}
	return h.p.prompt.Revision
}

// AwaitPrompt registers a pending request with the run that owns ctx. The
// run's control surfaces stay on screen and present it, while new input
// from the run waits in WaitForPrompts. Without a run the request remains
// answerable through its other views only.
func AwaitPrompt(ctx context.Context, p Prompt, respond PromptResponder) *PendingPrompt {
	f, _ := ctx.Value(flowKey{}).(*flow)
	if f == nil || respond == nil {
		return &PendingPrompt{}
	}
	p.Revision = promptRevision.Add(1)
	p.Session = f.session
	if p.Since.IsZero() {
		p.Since = time.Now()
	}
	pending := &pendingPrompt{prompt: p, respond: respond}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return &PendingPrompt{}
	}
	f.prompts = append(f.prompts, pending)
	f.mu.Unlock()
	promptMu.Lock()
	promptFlows[f] = struct{}{}
	promptMu.Unlock()
	f.presentPrompts(0)
	promptChanged()
	return &PendingPrompt{f: f, p: pending}
}

// Done removes the request and moves the run on according to its outcome.
// It is idempotent and never resurrects a stopped run.
func (h *PendingPrompt) Done(outcome PromptOutcome) {
	if h == nil || h.f == nil {
		return
	}
	h.once.Do(func() {
		f := h.f
		f.mu.Lock()
		f.prompts = slices.DeleteFunc(f.prompts, func(p *pendingPrompt) bool { return p == h.p })
		empty := len(f.prompts) == 0
		closed := f.closed
		f.mu.Unlock()
		if empty {
			promptMu.Lock()
			delete(promptFlows, f)
			promptMu.Unlock()
		}
		if !closed {
			f.presentPrompts(outcome)
		}
		promptChanged()
	})
}

// head returns the displayed request under f.mu.
func (f *flow) headLocked() (Prompt, bool) {
	if len(f.prompts) == 0 {
		return Prompt{}, false
	}
	p := f.prompts[0].prompt
	p.Queued = len(f.prompts) - 1
	return p, true
}

// presentPrompts shows the queue head on the run's visible surface, or
// returns that surface to work after the last request resolved.
func (f *flow) presentPrompts(outcome PromptOutcome) {
	f.mu.Lock()
	head, ok := f.headLocked()
	var owned []*Manager
	for m := range f.managers {
		owned = append(owned, m)
	}
	f.mu.Unlock()
	for _, m := range owned {
		m.mu.Lock()
		changed := false
		if m.flow == f && m.event.Visible && m.event.Persistent {
			changed = m.showPromptLocked(head, ok, outcome)
		}
		m.mu.Unlock()
		if changed {
			m.signal()
		}
	}
}

// showPromptLocked attaches or detaches a request. Callers hold m.mu.
func (m *Manager) showPromptLocked(head Prompt, ok bool, outcome PromptOutcome) bool {
	if ok {
		p := head
		m.event.Prompt = &p
		state := StateAwaitQuestion
		if p.Kind == KindPermission {
			state = StateAwaitPermission
		}
		m.enterLocked(state)
		return true
	}
	if m.event.Prompt == nil && !m.event.State.Awaiting() {
		return false
	}
	m.event.Prompt = nil
	switch outcome {
	case OutcomeAnswered, OutcomeGranted:
		m.enterLocked(StateResuming)
	case OutcomeDenied:
		m.enterLocked(StateDenied)
	default:
		m.enterLocked(StateThinking)
	}
	return true
}

// Respond delivers a decision from this surface to the exact request it
// displays. Stale revisions, stopped runs and repeated submissions are
// rejected before any service is called.
func (m *Manager) Respond(revision uint64, response PromptResponse) error {
	m.mu.Lock()
	f := m.flow
	shown := m.event.Prompt
	valid := !m.closed && m.suspended == 0 && m.event.Visible && f != nil && shown != nil && shown.Revision == revision && revision != 0
	m.mu.Unlock()
	if !valid {
		return ErrPromptStale
	}
	f.mu.Lock()
	if f.closed || len(f.prompts) == 0 || f.prompts[0].prompt.Revision != revision {
		f.mu.Unlock()
		return ErrPromptStale
	}
	pending := f.prompts[0]
	if pending.delivering {
		f.mu.Unlock()
		return ErrPromptBusy
	}
	if !pending.prompt.accepts(response) {
		f.mu.Unlock()
		return ErrPromptInvalid
	}
	pending.delivering = true
	f.mu.Unlock()
	m.mu.Lock()
	if m.flow == f && m.event.Prompt != nil && m.event.Prompt.Revision == revision {
		m.enterLocked(StateDelivering)
	}
	m.mu.Unlock()
	m.signal()
	if pending.respond(response) {
		return nil
	}
	// Another view resolved it first; its own Done updates the surface.
	f.mu.Lock()
	pending.delivering = false
	f.mu.Unlock()
	return ErrPromptStale
}

// accepts validates a response against the request it answers.
func (p Prompt) accepts(r PromptResponse) bool {
	if p.Kind == KindPermission {
		return r.Answers == nil && slices.Contains(p.Permission.Decisions, r.Decision)
	}
	if r.Decision != DecisionNone || len(r.Answers) != len(p.Questions) {
		return false
	}
	for i, q := range p.Questions {
		a := r.Answers[i]
		if a.QuestionID != q.ID || utf8.RuneCountInString(a.Text) > MaxPromptText || !utf8.ValidString(a.Text) {
			return false
		}
		switch q.Type {
		case QuestionYesNo:
			if a.Yes == nil || len(a.Selected) > 0 {
				return false
			}
		case QuestionFreeText:
			if a.Yes != nil || len(a.Selected) > 0 {
				return false
			}
		case QuestionSingleChoice, QuestionMultiChoice:
			if a.Yes != nil || (q.Type == QuestionSingleChoice && len(a.Selected) > 1) {
				return false
			}
			seen := map[string]bool{}
			for _, id := range a.Selected {
				if seen[id] || !slices.ContainsFunc(q.Choices, func(c PromptChoice) bool { return c.ID == id }) {
					return false
				}
				seen[id] = true
			}
		default:
			return false
		}
	}
	return true
}

// WaitForPrompts holds new input while the caller's run awaits the user.
// For the shared desktop it also waits for another run that is presenting
// a request there, so no agent acts between a question and its answer.
func WaitForPrompts(ctx context.Context, desktop bool) error {
	for {
		wake, blocked := promptGate(ctx, desktop)
		if !blocked {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		}
	}
}

// PromptBlocks is the non-blocking form of WaitForPrompts.
func PromptBlocks(ctx context.Context, desktop bool) bool {
	_, blocked := promptGate(ctx, desktop)
	return blocked
}

func promptGate(ctx context.Context, desktop bool) (<-chan struct{}, bool) {
	own, _ := ctx.Value(flowKey{}).(*flow)
	// Snapshot under promptMu, then inspect flows without it: stopping
	// runs take f.mu before promptMu.
	promptMu.Lock()
	wake := promptWake
	flows := make([]*flow, 0, len(promptFlows))
	for f := range promptFlows {
		flows = append(flows, f)
	}
	promptMu.Unlock()
	return wake, slices.ContainsFunc(flows, func(f *flow) bool {
		if f == own {
			return f.pending()
		}
		return desktop && (own == nil || f.session != own.session) && f.pending() && f.displaysPrompt()
	})
}

func (f *flow) pending() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.closed && len(f.prompts) > 0
}

// displaysPrompt reports whether one of the run's control surfaces is on
// screen; only then does it own the desktop while awaiting the user.
func (f *flow) displaysPrompt() bool {
	f.mu.Lock()
	owned := make([]*Manager, 0, len(f.managers))
	for m := range f.managers {
		owned = append(owned, m)
	}
	f.mu.Unlock()
	for _, m := range owned {
		m.mu.Lock()
		shown := m.flow == f && m.event.Visible && m.event.Prompt != nil
		m.mu.Unlock()
		if shown {
			return true
		}
	}
	return false
}

// AwaitingUser reports whether the run owning ctx has a pending request.
func AwaitingUser(ctx context.Context) bool {
	f, _ := ctx.Value(flowKey{}).(*flow)
	return f != nil && f.pending()
}

// dropPromptsLocked withdraws every request of a stopping run. Their
// services see cancellation through the run context. Callers hold f.mu.
func (f *flow) dropPromptsLocked() {
	if len(f.prompts) == 0 {
		return
	}
	f.prompts = nil
	promptMu.Lock()
	delete(promptFlows, f)
	promptMu.Unlock()
	go promptChanged()
}
