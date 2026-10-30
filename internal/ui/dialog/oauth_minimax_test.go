package dialog

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/minimax"
	"github.com/stretchr/testify/require"
)

func TestOAuthMiniMaxName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "MiniMax Token Plan", (&OAuthMiniMax{region: minimax.Global}).name())
	require.Equal(t, "MiniMax Token Plan (China)", (&OAuthMiniMax{region: minimax.China}).name())
}

func TestOAuthMiniMaxStopPollingWithoutStart(t *testing.T) {
	t.Parallel()
	require.Nil(t, (&OAuthMiniMax{}).stopPolling())
}

func TestOAuthMiniMaxStopPollingCancels(t *testing.T) {
	t.Parallel()
	m := &OAuthMiniMax{}
	cancelled := false
	m.cancelFunc = func() { cancelled = true }
	require.Nil(t, m.stopPolling())
	require.True(t, cancelled)
}
