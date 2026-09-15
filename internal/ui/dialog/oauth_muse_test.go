package dialog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOAuthMuseName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Muse", (&OAuthMuse{}).name())
}

func TestOAuthMuseStopPollingWithoutStart(t *testing.T) {
	t.Parallel()
	// Cancelling before polling starts must be a safe no-op: the
	// dialog can close while the device code is still on screen.
	require.Nil(t, (&OAuthMuse{}).stopPolling())
}

func TestOAuthMuseStopPollingCancels(t *testing.T) {
	t.Parallel()
	m := &OAuthMuse{}
	cancelled := false
	m.cancelFunc = func() { cancelled = true }
	require.Nil(t, m.stopPolling())
	require.True(t, cancelled)
}
