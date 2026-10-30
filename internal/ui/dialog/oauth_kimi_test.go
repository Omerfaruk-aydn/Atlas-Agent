package dialog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOAuthKimiName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Kimi Code", (&OAuthKimi{}).name())
}

func TestOAuthKimiStopPollingWithoutStart(t *testing.T) {
	t.Parallel()
	// Cancelling before polling starts must be a safe no-op: the
	// dialog can close while the device code is still on screen.
	require.Nil(t, (&OAuthKimi{}).stopPolling())
}

func TestOAuthKimiStopPollingCancels(t *testing.T) {
	t.Parallel()
	m := &OAuthKimi{}
	cancelled := false
	m.cancelFunc = func() { cancelled = true }
	require.Nil(t, m.stopPolling())
	require.True(t, cancelled)
}
