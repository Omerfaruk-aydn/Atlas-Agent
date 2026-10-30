package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExplicitTurnCommand(t *testing.T) {
	t.Parallel()
	steer, handled := explicitTurnCommand("/steer prefer the existing adapter")
	require.True(t, handled)
	require.Contains(t, steer, "active turn")
	require.Contains(t, steer, "existing adapter")
	queued, handled := explicitTurnCommand("/queue update the docs")
	require.True(t, handled)
	require.Equal(t, newTurnMarker+"update the docs", queued)
	_, handled = explicitTurnCommand("/queue")
	require.True(t, handled)
	ordinary, handled := explicitTurnCommand("ordinary prompt")
	require.False(t, handled)
	require.Equal(t, "ordinary prompt", ordinary)
}
