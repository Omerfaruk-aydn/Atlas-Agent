package android

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObservedTextAndKeysRejectAppSwitchAndConsumedObservations(t *testing.T) {
	f := newFake(t)
	c := NewController(f.adb)
	obs, err := c.Observe(t.Context(), "emulator-5554", true, false)
	require.NoError(t, err)
	originalFocus := f.state.Focus
	f.state.Focus = "com.other.app/.Main"
	f.save()
	_, err = c.TypeTextObserved(t.Context(), "emulator-5554", "private input", obs.ID)
	var stale *StaleError
	require.ErrorAs(t, err, &stale)
	require.ErrorAs(t, c.KeyObserved(t.Context(), "emulator-5554", "enter", obs.ID), &stale)
	require.Empty(t, inputCommands(f.commands()))
	f.state.Focus = originalFocus
	f.save()
	obs, err = c.Observe(t.Context(), "emulator-5554", true, false)
	require.NoError(t, err)
	_, err = c.TypeTextObserved(t.Context(), "emulator-5554", "saved", obs.ID)
	require.NoError(t, err)
	require.ErrorAs(t, c.KeyObserved(t.Context(), "emulator-5554", "enter", obs.ID), &stale)
	require.Len(t, inputCommands(f.commands()), 1)
	obs, err = c.Observe(t.Context(), "emulator-5554", true, false)
	require.NoError(t, err)
	require.NoError(t, c.KeyObserved(t.Context(), "emulator-5554", "enter", obs.ID))
	require.ErrorAs(t, c.KeyObserved(t.Context(), "emulator-5554", "enter", obs.ID), &stale)
	require.Len(t, inputCommands(f.commands()), 2)
}
