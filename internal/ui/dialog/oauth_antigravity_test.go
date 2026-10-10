package dialog

import (
	"context"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/stretchr/testify/require"
)

func TestAntigravityExpiredLoginReportsError(t *testing.T) {
	t.Parallel()
	provider := &OAuthAntigravity{session: expiredAntigravitySession{}}
	msg := provider.startPolling("", 0)()
	failure, ok := msg.(ActionOAuthErrored)
	require.True(t, ok, "An expired login must terminate the spinner with an error")
	require.True(t, errors.Is(failure.Error, context.DeadlineExceeded))
}

type expiredAntigravitySession struct{}

func (expiredAntigravitySession) WaitWithProgress(ctx context.Context, _ func(string)) (*oauth.Token, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (expiredAntigravitySession) Close() {}

func TestAntigravityStoppedLoginDoesNotRestart(t *testing.T) {
	t.Parallel()
	provider := &OAuthAntigravity{}
	provider.stopPolling()
	require.Nil(t, provider.startPolling("", 60)())
}
