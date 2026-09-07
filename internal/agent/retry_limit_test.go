package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/stretchr/testify/require"
)

// A config that never mentions retries must leave the provider library's
// default in place, not silently disable retrying.
func TestUnsetRetryBudgetLeavesTheDefaultAlone(t *testing.T) {
	a := &sessionAgent{maxProviderRetries: csync.NewValue(ptrBox[int]{})}
	require.Nil(t, a.maxRetries())
}

func TestConfiguredRetryBudgetIsPassedThrough(t *testing.T) {
	n := 7
	a := &sessionAgent{maxProviderRetries: csync.NewValue(ptrBox[int]{v: &n})}
	got := a.maxRetries()
	require.NotNil(t, got)
	require.Equal(t, 7, *got)
}

// Zero is a real setting -- "do not retry" -- and must survive, unlike an
// absent one.
func TestZeroRetryBudgetDisablesRetries(t *testing.T) {
	n := 0
	a := &sessionAgent{maxProviderRetries: csync.NewValue(ptrBox[int]{v: &n})}
	got := a.maxRetries()
	require.NotNil(t, got)
	require.Equal(t, 0, *got)
}

func TestNegativeRetryBudgetIsClampedToNone(t *testing.T) {
	n := -3
	a := &sessionAgent{maxProviderRetries: csync.NewValue(ptrBox[int]{v: &n})}
	got := a.maxRetries()
	require.NotNil(t, got)
	require.Equal(t, 0, *got)
}

// The library must never be handed a pointer into the agent's own state.
func TestRetryBudgetIsCopied(t *testing.T) {
	n := 4
	a := &sessionAgent{maxProviderRetries: csync.NewValue(ptrBox[int]{v: &n})}
	got := a.maxRetries()
	*got = 99
	require.Equal(t, 4, n)
}

// A live config change to the retry budget must reach an already-running
// session -- see SetLimits.
func TestSetLimitsUpdatesRetryBudgetLive(t *testing.T) {
	a := &sessionAgent{
		maxProviderRetries: csync.NewValue(ptrBox[int]{}),
		maxSessionCost:     csync.NewValue(0.0),
		maxStepsPerTurn:    csync.NewValue(0),
	}
	require.Nil(t, a.maxRetries())

	n := 5
	a.SetLimits(&n, 12.5, 40)

	got := a.maxRetries()
	require.NotNil(t, got)
	require.Equal(t, 5, *got)
	require.InDelta(t, 12.5, a.maxSessionCost.Get(), 0)
	require.Equal(t, 40, a.maxStepsPerTurn.Get())
}
