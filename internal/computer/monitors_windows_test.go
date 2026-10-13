//go:build windows

package computer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Real desktop: monitor bounds share the screenshot's physical coordinate
// space, so together they span exactly the virtual screen that
// ScreenSize (and every capture) reports.
func TestNativeMonitorsMatchThePhysicalVirtualScreen(t *testing.T) {
	b, err := openPlatform()
	require.NoError(t, err)
	wb := b.(*windowsBackend)
	raw, err := wb.Automation(context.Background(), AutomationRequest{Action: "monitors"})
	require.NoError(t, err)
	var monitors []MonitorObservation
	require.NoError(t, json.Unmarshal(raw, &monitors))
	require.NotEmpty(t, monitors)
	size, err := wb.ScreenSize()
	require.NoError(t, err)
	_, _, w, h := VirtualBounds(monitors)
	require.Equal(t, size.Width, w, "monitor bounds must be physical like the capture (got %+v)", monitors)
	require.Equal(t, size.Height, h)
	for _, m := range monitors {
		require.GreaterOrEqual(t, m.DPI, 96)
		require.InDelta(t, float64(m.DPI)/96, m.ScaleFactor, 1e-9)
		require.Equal(t, CoordinateSpacePhysical, m.CoordinateSpace)
	}
}
