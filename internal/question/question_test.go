package question

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func batch(id string) Request {
	return Request{ID: id, Questions: []Question{{ID: id + "-q", Type: TypeFreeText, Text: "Why?", Description: "Explain"}}}
}

func TestConcurrentRequestsQueueAndResolveByID(t *testing.T) {
	svc := NewService()
	events := svc.Subscribe(t.Context())
	type result struct {
		answers []Answer
		err     error
	}
	first, second := make(chan result, 1), make(chan result, 1)
	go func() { a, err := svc.Ask(t.Context(), batch("one")); first <- result{a, err} }()
	require.Equal(t, "one", (<-events).Payload.ID)
	go func() { a, err := svc.Ask(t.Context(), batch("two")); second <- result{a, err} }()
	select {
	case e := <-events:
		t.Fatalf("Queued request %s must wait for the displayed one", e.Payload.ID)
	case <-time.After(50 * time.Millisecond):
	}
	require.Eventually(t, func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return len(svc.queue) == 2
	}, time.Second, time.Millisecond)

	// Answers are routed by question ID, never to whichever request is newest.
	require.True(t, svc.AnswerRequest("two", []Answer{{QuestionID: "two-q", FillInText: "second"}}))
	require.Equal(t, "second", (<-second).answers[0].FillInText)
	require.False(t, svc.AnswerRequest("two", []Answer{{QuestionID: "two-q"}}), "A request resolves once")
	require.True(t, svc.Answer([]Answer{{QuestionID: "one-q", FillInText: "first"}}))
	require.Equal(t, "first", (<-first).answers[0].FillInText)
	require.False(t, svc.Answer([]Answer{{QuestionID: "one-q", FillInText: "again"}}), "A double submit is rejected")
}

func TestNextRequestIsShownAfterTheDisplayedOneResolves(t *testing.T) {
	svc := NewService()
	events := svc.Subscribe(t.Context())
	notifications := svc.SubscribeNotifications(t.Context())
	go svc.Ask(t.Context(), batch("a"))
	require.Equal(t, "a", (<-events).Payload.ID)
	go svc.Ask(t.Context(), batch("b"))
	require.Eventually(t, func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return len(svc.queue) == 2
	}, time.Second, time.Millisecond)
	require.True(t, svc.CancelRequest("a"))
	require.Equal(t, "a", (<-notifications).Payload.BatchID)
	require.Equal(t, "b", (<-events).Payload.ID)
	require.False(t, svc.CancelRequest("a"))
	require.True(t, svc.Cancel())
}
