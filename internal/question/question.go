// Package question provides services for asking the user questions
// via the TUI and blocking until an answer is received. It mirrors
// the permission service pattern: publish a request over pubsub,
// block on a channel, and resolve when the UI sends back answers.
//
// Only one question can be pending at a time (the tool blocks until
// answered), so no correlation IDs are needed in the domain model.
package question

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/google/uuid"
)

// ErrCancelled is returned by Ask when the user cancels the question.
var ErrCancelled = errors.New("question cancelled by user")

// Type identifies the kind of question to present.
type Type string

const (
	TypeYesNo        Type = "yes_no"
	TypeSingleChoice Type = "single_choice"
	TypeMultiChoice  Type = "multi_choice"
	TypeFreeText     Type = "free_text"
)

// Choice represents a single selectable option.
type Choice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Question is a single question definition within a Request.
type Question struct {
	ID          string   `json:"id"`
	Type        Type     `json:"type"`
	Label       string   `json:"label,omitempty"`
	Text        string   `json:"question"`
	Description string   `json:"description,omitempty"`
	Choices     []Choice `json:"choices,omitempty"`
}

// Answer carries the user's response to a single Question.
type Answer struct {
	QuestionID  string            `json:"question_id"`
	SelectedIDs []string          `json:"selected_ids,omitempty"`
	FillInText  string            `json:"fill_in_text,omitempty"`
	Yes         *bool             `json:"yes,omitempty"`
	Notes       map[string]string `json:"notes,omitempty"`
}

// HasNotes reports whether any notes were attached.
func (a Answer) HasNotes() bool { return len(a.Notes) > 0 }

// Request is the service envelope published to the UI. It contains
// one or more Questions. A single question renders without tabs;
// multiple questions render as a tabbed form with confirmation.
type Request struct {
	ID                 string     `json:"id"`
	SessionID          string     `json:"session_id"`
	ToolCallID         string     `json:"tool_call_id"`
	Questions          []Question `json:"questions"`
	ConfirmTitle       string     `json:"confirm_title,omitempty"`
	ConfirmDescription string     `json:"confirm_description,omitempty"`
}

// Validate checks that a Request has valid fields. For multiple
// questions, ConfirmTitle and ConfirmDescription are required.
func (r Request) Validate() error {
	if len(r.Questions) == 0 {
		return fmt.Errorf("at least one question is required")
	}
	if len(r.Questions) > MaxQuestions {
		return fmt.Errorf("questions exceed maximum of %d (got %d)", MaxQuestions, len(r.Questions))
	}
	for i, q := range r.Questions {
		if err := q.Validate(); err != nil {
			return fmt.Errorf("question %d: %w", i+1, err)
		}
	}
	return nil
}

// Validate checks that a Question has valid fields. Error messages
// are written for LLM consumption: specific and actionable.
func (q Question) Validate() error {
	label := q.identifier()
	if q.Text == "" {
		return fmt.Errorf("%s: question text is required", label)
	}
	if len(q.Text) > MaxQuestionLength {
		return fmt.Errorf("%s: text exceeds %d characters (got %d)", label, MaxQuestionLength, len(q.Text))
	}
	if q.Description == "" {
		return fmt.Errorf("%s: description is required", label)
	}
	if len(q.Description) > MaxDescriptionLength {
		return fmt.Errorf("%s: description exceeds %d characters (got %d)", label, MaxDescriptionLength, len(q.Description))
	}
	switch q.Type {
	case TypeYesNo, TypeFreeText:
		// No choices needed.
	case TypeSingleChoice, TypeMultiChoice:
		if len(q.Choices) < 2 {
			return fmt.Errorf("%s: %s requires at least 2 choices in the \"choices\" array (got %d). Use \"choices\", not \"options\"", label, q.Type, len(q.Choices))
		}
		if len(q.Choices) > MaxChoices {
			return fmt.Errorf("%s: choices exceed maximum of %d (got %d)", label, MaxChoices, len(q.Choices))
		}
		seen := make(map[string]bool, len(q.Choices))
		for i, c := range q.Choices {
			if c.ID == "" {
				return fmt.Errorf("%s: choice %d must have an \"id\" field", label, i+1)
			}
			if seen[c.ID] {
				return fmt.Errorf("%s: choice %d has duplicate id %q", label, i+1, c.ID)
			}
			seen[c.ID] = true
			if c.Label == "" {
				return fmt.Errorf("%s: choice %d (%s) must have a \"label\" field", label, i+1, c.ID)
			}
			if len(c.Label) > MaxChoiceLabelLength {
				return fmt.Errorf("%s: choice %d label exceeds %d characters (got %d)", label, i+1, MaxChoiceLabelLength, len(c.Label))
			}
			if len(c.Description) > MaxChoiceDescriptionLength {
				return fmt.Errorf("%s: choice %d description exceeds %d characters (got %d)", label, i+1, MaxChoiceDescriptionLength, len(c.Description))
			}
		}
	default:
		return fmt.Errorf("%s: unknown type %q (must be yes_no, single_choice, multi_choice, or free_text)", label, q.Type)
	}
	return nil
}

// identifier returns a human-readable label for error messages.
// Uses the question label, text excerpt, or a fallback.
func (q Question) identifier() string {
	if q.Label != "" {
		return fmt.Sprintf("[%s]", q.Label)
	}
	if q.Text != "" {
		t := q.Text
		if len(t) > 40 {
			t = t[:40] + "…"
		}
		return fmt.Sprintf("[%s]", t)
	}
	return "[unnamed question]"
}

const (
	MaxQuestionLength          = 240
	MaxDescriptionLength       = 600
	MaxChoiceLabelLength       = 200
	MaxChoiceDescriptionLength = 200
	MaxChoices                 = 5
	MaxQuestions               = 5
)

// Notification is published when a question batch is resolved so
// that non-answering clients can dismiss their open forms.
type Notification struct {
	BatchID string `json:"batch_id"`
}

// Service manages the lifecycle of question requests. Requests from
// concurrent runs queue in arrival order: subscribers see the oldest
// pending request, and each request resolves exactly once by its ID.
type Service interface {
	pubsub.Subscriber[Request]

	// SubscribeNotifications returns a channel for question
	// resolution notifications.
	SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[Notification]

	// Ask publishes questions and blocks until the user answers
	// or the context is cancelled.
	Ask(ctx context.Context, req Request) ([]Answer, error)

	// Answer resolves the pending request whose questions the answers
	// belong to, or the displayed request when answers carry no IDs.
	Answer(answers []Answer) bool

	// AnswerRequest resolves exactly the pending request with this ID.
	AnswerRequest(id string, answers []Answer) bool

	// Cancel cancels the displayed request. Returns false if no
	// question is pending.
	Cancel() bool

	// CancelRequest cancels exactly the pending request with this ID.
	CancelRequest(id string) bool
}

type pendingBatch struct {
	req       Request
	answers   chan []Answer
	cancelled chan struct{}
	resolved  bool
}

type questionService struct {
	broker             *pubsub.Broker[Request]
	notificationBroker *pubsub.Broker[Notification]
	mu                 sync.Mutex
	queue              []*pendingBatch
}

// NewService creates a new question service.
func NewService() *questionService {
	return &questionService{
		broker:             pubsub.NewBroker[Request](),
		notificationBroker: pubsub.NewBroker[Notification](),
	}
}

// Subscribe returns a channel for question events.
func (s *questionService) Subscribe(ctx context.Context) <-chan pubsub.Event[Request] {
	return s.broker.Subscribe(ctx)
}

// SubscribeNotifications returns a channel for question resolution
// notifications.
func (s *questionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[Notification] {
	return s.notificationBroker.Subscribe(ctx)
}

type pendingHookKey struct{}

// WithPendingHook runs hook with the request once Ask has registered it,
// so another view can only answer a request that is already pending.
func WithPendingHook(ctx context.Context, hook func(Request)) context.Context {
	return context.WithValue(ctx, pendingHookKey{}, hook)
}

// Prepare assigns missing IDs and confirm defaults, then validates.
func Prepare(req Request) (Request, error) {
	if req.ID == "" {
		req.ID = uuid.New().String()
	}
	req.Questions = append([]Question(nil), req.Questions...)
	for i := range req.Questions {
		if req.Questions[i].ID == "" {
			req.Questions[i].ID = uuid.New().String()
		}
	}

	// Apply defaults for multi-question confirm fields.
	if len(req.Questions) >= 2 {
		if req.ConfirmTitle == "" {
			req.ConfirmTitle = "Ready to go?"
		}
		if req.ConfirmDescription == "" {
			req.ConfirmDescription = "Review your answers above and confirm."
		}
	}
	return req, req.Validate()
}

// Ask publishes a request and blocks until the user answers.
func (s *questionService) Ask(ctx context.Context, req Request) ([]Answer, error) {
	req, err := Prepare(req)
	if err != nil {
		return nil, err
	}

	batch := &pendingBatch{req: req, answers: make(chan []Answer, 1), cancelled: make(chan struct{})}
	s.mu.Lock()
	for _, other := range s.queue {
		if other.req.ID == req.ID {
			s.mu.Unlock()
			return nil, fmt.Errorf("question request %s is already pending", req.ID)
		}
	}
	s.queue = append(s.queue, batch)
	head := len(s.queue) == 1
	s.mu.Unlock()

	defer s.withdraw(batch)

	if head {
		s.broker.Publish(pubsub.CreatedEvent, req)
	}
	if hook, ok := ctx.Value(pendingHookKey{}).(func(Request)); ok && hook != nil {
		hook(req)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-batch.cancelled:
		return nil, ErrCancelled
	case answers := <-batch.answers:
		return answers, nil
	}
}

// withdraw removes a finished request and shows the next one. A request
// abandoned by its run is withdrawn from every view.
func (s *questionService) withdraw(batch *pendingBatch) {
	s.mu.Lock()
	wasHead := len(s.queue) > 0 && s.queue[0] == batch
	abandoned := !batch.resolved
	batch.resolved = true
	s.queue = slices.DeleteFunc(s.queue, func(b *pendingBatch) bool { return b == batch })
	var next *Request
	if wasHead && len(s.queue) > 0 {
		req := s.queue[0].req
		next = &req
	}
	s.mu.Unlock()
	if abandoned {
		s.notificationBroker.Publish(pubsub.CreatedEvent, Notification{BatchID: batch.req.ID})
	}
	if next != nil {
		s.broker.Publish(pubsub.CreatedEvent, *next)
	}
}

// take marks one pending request resolved; the first caller wins.
func (s *questionService) take(match func(*pendingBatch) bool) *pendingBatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, batch := range s.queue {
		if !batch.resolved && match(batch) {
			batch.resolved = true
			return batch
		}
	}
	return nil
}

func (s *questionService) resolved(batch *pendingBatch) {
	// Publish a notification so non-answering clients can dismiss
	// their open question forms.
	s.notificationBroker.Publish(pubsub.CreatedEvent, Notification{BatchID: batch.req.ID})
}

// Answer resolves the pending request the answers belong to. Returns
// false if it is no longer pending (already answered or cancelled).
func (s *questionService) Answer(answers []Answer) bool {
	ids := make(map[string]bool, len(answers))
	for _, answer := range answers {
		if answer.QuestionID != "" {
			ids[answer.QuestionID] = true
		}
	}
	first := true
	batch := s.take(func(b *pendingBatch) bool {
		if len(ids) == 0 {
			// Legacy callers without IDs answer the displayed request.
			matched := first
			first = false
			return matched
		}
		for _, q := range b.req.Questions {
			if ids[q.ID] {
				return true
			}
		}
		return false
	})
	if batch == nil {
		return false
	}
	batch.answers <- answers
	s.resolved(batch)
	return true
}

// AnswerRequest resolves exactly the request with this ID.
func (s *questionService) AnswerRequest(id string, answers []Answer) bool {
	batch := s.take(func(b *pendingBatch) bool { return b.req.ID == id })
	if batch == nil {
		return false
	}
	batch.answers <- answers
	s.resolved(batch)
	return true
}

// Cancel cancels the displayed request. Returns false if no
// question is pending.
func (s *questionService) Cancel() bool {
	first := true
	batch := s.take(func(*pendingBatch) bool {
		matched := first
		first = false
		return matched
	})
	if batch == nil {
		return false
	}
	close(batch.cancelled)
	s.resolved(batch)
	return true
}

// CancelRequest cancels exactly the request with this ID.
func (s *questionService) CancelRequest(id string) bool {
	batch := s.take(func(b *pendingBatch) bool { return b.req.ID == id })
	if batch == nil {
		return false
	}
	close(batch.cancelled)
	s.resolved(batch)
	return true
}
