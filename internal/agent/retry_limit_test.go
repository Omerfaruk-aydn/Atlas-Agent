package agent

import (
	"testing"
	"time"

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

// A live config change to fallback_cooldown must reach an already-running
// session -- see SetFallbackCooldown.
func TestSetFallbackCooldownUpdatesLive(t *testing.T) {
	a := &sessionAgent{fallbackCooldown: csync.NewValue(time.Duration(0))}
	require.Equal(t, time.Duration(0), a.fallbackCooldown.Get())

	a.SetFallbackCooldown(30 * time.Second)
	require.Equal(t, 30*time.Second, a.fallbackCooldown.Get())
}

// A live config change must be able to lower or clear a limit that was
// previously set, not just raise one from zero -- mirrors the "off" tests
// added for hooks and the advisor.
func TestSetLimitsCanLowerAnAlreadyConfiguredLimit(t *testing.T) {
	n := 5
	a := &sessionAgent{
		maxProviderRetries: csync.NewValue(ptrBox[int]{v: &n}),
		maxSessionCost:     csync.NewValue(100.0),
		maxStepsPerTurn:    csync.NewValue(50),
	}
	require.Equal(t, 5, *a.maxRetries())

	a.SetLimits(nil, 1.0, 3)

	require.Nil(t, a.maxRetries(), "clearing max_provider_retries live must actually clear it")
	require.InDelta(t, 1.0, a.maxSessionCost.Get(), 0)
	require.Equal(t, 3, a.maxStepsPerTurn.Get())
}

// Mirrors TestSetLimitsCanLowerAnAlreadyConfiguredLimit for
// fallback_cooldown: a live change must be able to shorten (or clear) a
// cooldown that was already set, not just extend one from zero.
func TestSetFallbackCooldownCanShortenAnAlreadyConfiguredCooldown(t *testing.T) {
	a := &sessionAgent{fallbackCooldown: csync.NewValue(5 * time.Minute)}
	require.Equal(t, 5*time.Minute, a.fallbackCooldown.Get())

	a.SetFallbackCooldown(0)

	require.Equal(t, time.Duration(0), a.fallbackCooldown.Get())
}
