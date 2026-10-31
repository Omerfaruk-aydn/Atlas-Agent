package secretprompt

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAskIsUnavailableUntilAnInterfaceAttaches(t *testing.T) {
	t.Parallel()
	svc := NewService()
	_, err := svc.Ask(t.Context(), Request{Title: "API key"})
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestSubmitReturnsTheSecretOnlyToTheCaller(t *testing.T) {
	t.Parallel()
	svc := NewService()
	svc.SetAvailable(true)
	events := svc.Subscribe(t.Context())

	type outcome struct {
		secret string
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		s, err := svc.Ask(t.Context(), Request{Title: "API key", Detail: "sent to host"})
		done <- outcome{s, err}
	}()

	ev := <-events
	require.Equal(t, "API key", ev.Payload.Title)
	require.True(t, svc.Submit(ev.Payload.ID, "s3cret"))
	require.False(t, svc.Submit(ev.Payload.ID, "again"), "a request resolves once")

	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, "s3cret", got.secret)
}

func TestCancelAndContextEndAskWithoutASecret(t *testing.T) {
	t.Parallel()
	svc := NewService()
	svc.SetAvailable(true)
	events := svc.Subscribe(t.Context())

	errs := make(chan error, 1)
	go func() {
		_, err := svc.Ask(t.Context(), Request{Title: "API key"})
		errs <- err
	}()
	ev := <-events
	require.True(t, svc.Cancel(ev.Payload.ID))
	require.ErrorIs(t, <-errs, ErrCancelled)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := svc.Ask(ctx, Request{Title: "API key"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
