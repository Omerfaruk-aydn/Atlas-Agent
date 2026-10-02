package engineering

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActiveOperationUsesReservedCallAndRejectsChangedBudget(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	require.NoError(t, s.SetBudget(t.Context(), "session", "", Limits{MaxToolCalls: 1, MaxTokens: 10}))
	op, err := s.Begin(t.Context(), "session", "call", "scenario", "{}", "")
	require.NoError(t, err)
	require.NoError(t, s.CheckOperation(t.Context(), "session", op))
	_, err = s.Begin(t.Context(), "session", "second", "bash", "{}", "")
	require.Error(t, err)
	require.NoError(t, s.Charge(t.Context(), "session", "", 10, 0, 0))
	require.ErrorContains(t, s.CheckOperation(t.Context(), "session", op), "exhausted")
	require.NoError(t, s.Finish(t.Context(), "session", op, false, false))
	require.ErrorContains(t, s.CheckOperation(t.Context(), "session", op), "no longer active")
}
