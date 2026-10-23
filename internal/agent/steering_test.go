package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyTurnRouting(t *testing.T) {
	t.Parallel()
	call := applyTurnRouting(SessionAgentCall{Prompt: newTurnMarker + "run independently"})
	require.Equal(t, "run independently", call.Prompt)
	require.NotEmpty(t, call.RunID)
	existing := applyTurnRouting(SessionAgentCall{Prompt: newTurnMarker + "next", RunID: "fixed"})
	require.Equal(t, "fixed", existing.RunID)
	ordinary := applyTurnRouting(SessionAgentCall{Prompt: "steer this turn"})
	require.Empty(t, ordinary.RunID)
}
