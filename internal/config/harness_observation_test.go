package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHarnessAblationPreservesDefaultAndRejectsUnknownGates(t *testing.T) {
	t.Parallel()
	var unset *Harness
	require.True(t, unset.TraceEnabled())
	require.True(t, unset.GateEnabled("contract"))
	h := &Harness{DisabledGates: []string{"contract", "goal_judge"}}
	require.NoError(t, h.ValidateAblation())
	require.False(t, h.GateEnabled("contract"))
	require.True(t, h.GateEnabled("evaluator"))
	h.DisabledGates = []string{"mutation"}
	require.NoError(t, h.ValidateAblation())
	h.DisabledGates = []string{"imaginary"}
	require.Error(t, h.ValidateAblation())
	require.False(t, unset.MutationEnabled())
	require.False(t, unset.LintFixEnabled())
}
