//go:build windows

package activity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMissingCursorSurfaceDoesNotAttemptSystemOverride(t *testing.T) {
	r := newPlatformRenderer().(*windowsRenderer)
	r.event = Event{Visible: true, Persistent: true}
	r.draw()
	require.False(t, r.cursor.disabled)
	require.Nil(t, r.cursor.guard)
}
