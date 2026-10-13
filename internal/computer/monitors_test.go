package computer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A secondary monitor left of the primary has negative coordinates; both
// share the physical virtual-screen space.
func TestMonitorGeometryCoversNegativeCoordinates(t *testing.T) {
	t.Parallel()
	monitors := []MonitorObservation{
		{Name: `\.\DISPLAY1`, Primary: true, X: 0, Y: 0, Width: 1920, Height: 1080, DPI: 120, ScaleFactor: 1.25},
		{Name: `\.\DISPLAY2`, X: -2560, Y: -200, Width: 2560, Height: 1440, DPI: 96, ScaleFactor: 1},
	}
	x, y, w, h := VirtualBounds(monitors)
	require.Equal(t, [4]int{-2560, -200, 4480, 1440}, [4]int{x, y, w, h})
	m, ok := MonitorAt(monitors, -10, 0)
	require.True(t, ok)
	require.Equal(t, `\.\DISPLAY2`, m.Name)
	m, ok = MonitorAt(monitors, 1919, 1079)
	require.True(t, ok)
	require.True(t, m.Primary)
	_, ok = MonitorAt(monitors, 1920, 0)
	require.False(t, ok)
	_, ok = MonitorAt(monitors, 0, -1)
	require.False(t, ok, "x=0 is past the secondary monitor's right edge and y=-1 is above the primary")
}
