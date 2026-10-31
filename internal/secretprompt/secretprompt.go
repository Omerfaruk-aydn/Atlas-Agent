// Package secretprompt asks the user for a secret (an API key) through a
// masked input in the interface, and hands the value straight to the Go
// caller that asked for it.
//
// It exists next to the question service on purpose. A question's answer
// is stored in the conversation and sent to the model, which is exactly
// what must never happen to a credential. Here the request that travels
// over pubsub carries only a title and a detail line; the secret comes
// back through Submit, a direct method call, and is returned from Ask to
// the tool or command that needed it. It is never published, persisted or
// logged by this package.
package secretprompt

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/google/uuid"
)

var (
	// ErrCancelled is returned by Ask when the user dismisses the prompt.
	ErrCancelled = errors.New("secret prompt cancelled by user")
	// ErrUnavailable is returned by Ask when no interface is attached that
	// can show a masked input.
	ErrUnavailable = errors.New("no interface is available to ask for a secret")
)

// Request is what the interface shows. It never contains the secret.
type Request struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	// Title is the dialog heading, e.g. "API key for acme".
	Title string `json:"title"`
	// Detail says where the secret will be sent and stored.
	Detail string `json:"detail,omitempty"`
}

// Notification is published when a request is resolved or abandoned so
// that an open dialog can be dismissed.
type Notification struct {
	ID string `json:"id"`
}

// Service mediates between a caller that needs a secret and the interface
// that can ask for it.
type Service interface {
	pubsub.Subscriber[Request]

	// SubscribeNotifications returns a channel of resolution notices.
	SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[Notification]

	// Available reports whether an interface is attached that will show
	// the prompt. Callers check it first so they can fail fast with an
	// actionable message instead of waiting on nobody.
	Available() bool

	// SetAvailable is called by the interface when it starts or stops
	// listening.
	SetAvailable(bool)

	// Ask publishes the request and blocks until the user submits a
	// secret, cancels, or ctx ends.
	Ask(ctx context.Context, req Request) (string, error)

	// Submit resolves the pending request with the secret the user typed.
	// It returns false when the request is no longer pending.
	Submit(id, secret string) bool

	// Cancel dismisses the pending request.
	Cancel(id string) bool
}

type pending struct {
	req    Request
	result chan result
}

type result struct {
	secret    string
	cancelled bool
}

type service struct {
	broker       *pubsub.Broker[Request]
	notification *pubsub.Broker[Notification]
	available    atomic.Bool

	mu      sync.Mutex
	pending map[string]*pending
}

// NewService creates a secret prompt service. It reports itself as
// unavailable until an interface calls SetAvailable(true).
func NewService() Service {
	return &service{
		broker:       pubsub.NewBroker[Request](),
		notification: pubsub.NewBroker[Notification](),
		pending:      map[string]*pending{},
	}
}

func (s *service) Subscribe(ctx context.Context) <-chan pubsub.Event[Request] {
	return s.broker.Subscribe(ctx)
}

func (s *service) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[Notification] {
	return s.notification.Subscribe(ctx)
}

func (s *service) Available() bool { return s.available.Load() }

func (s *service) SetAvailable(v bool) { s.available.Store(v) }

func (s *service) Ask(ctx context.Context, req Request) (string, error) {
	if !s.Available() {
		return "", ErrUnavailable
	}
	if req.Title == "" {
		return "", fmt.Errorf("secret prompt requires a title")
	}
	if req.ID == "" {
		req.ID = uuid.New().String()
	}
	p := &pending{req: req, result: make(chan result, 1)}

	s.mu.Lock()
	if _, dup := s.pending[req.ID]; dup {
		s.mu.Unlock()
		return "", fmt.Errorf("secret prompt %s is already pending", req.ID)
	}
	s.pending[req.ID] = p
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, req.ID)
		s.mu.Unlock()
		s.notification.Publish(pubsub.CreatedEvent, Notification{ID: req.ID})
	}()

	s.broker.Publish(pubsub.CreatedEvent, req)

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-p.result:
		if r.cancelled {
			return "", ErrCancelled
		}
		return r.secret, nil
	}
}

func (s *service) take(id string) *pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pending[id]
	delete(s.pending, id)
	return p
}

func (s *service) Submit(id, secret string) bool {
	p := s.take(id)
	if p == nil {
		return false
	}
	p.result <- result{secret: secret}
	return true
}

func (s *service) Cancel(id string) bool {
	p := s.take(id)
	if p == nil {
		return false
	}
	p.result <- result{cancelled: true}
	return true
}
