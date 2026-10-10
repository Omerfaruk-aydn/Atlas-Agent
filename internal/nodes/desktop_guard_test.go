package nodes

import (
	"sync/atomic"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

type foregroundDesktop struct {
	*fakeDesktop
	current atomic.Value
}

func (f *foregroundDesktop) ForegroundWindow() string { return f.current.Load().(string) }

func TestDesktopInputRejectsForegroundChangesBeforeSendingInput(t *testing.T) {
	t.Parallel()
	desk := &foregroundDesktop{fakeDesktop: &fakeDesktop{size: computer.Size{Width: 800, Height: 600}}}
	desk.current.Store("123")
	c := newHarness(t, []Capability{CapDesktop}, desk).pair(t)
	obs, err := c.Observe(t.Context(), false)
	require.NoError(t, err)
	desk.current.Store("456")
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "guard-click", ObservationID: obs.ObservationID, WindowID: "123", Action: "click", X: 10, Y: 10})
	require.Equal(t, "wrong_window", apiCode(err))
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "guard-type", ObservationID: obs.ObservationID, WindowID: "123", Action: "type", Text: "private input"})
	require.Equal(t, "wrong_window", apiCode(err))
	require.Zero(t, desk.clicks.Load())
	require.Empty(t, desk.typed)
	desk.current.Store("123")
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "guard-click", ObservationID: obs.ObservationID, WindowID: "123", Action: "click", X: 10, Y: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, desk.clicks.Load())
}

func TestDesktopInputCannotClaimAWindowGuardWithoutForegroundSupport(t *testing.T) {
	t.Parallel()
	desk := &fakeDesktop{size: computer.Size{Width: 800, Height: 600}}
	c := newHarness(t, []Capability{CapDesktop}, desk).pair(t)
	obs, err := c.Observe(t.Context(), false)
	require.NoError(t, err)
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "unsupported-guard", ObservationID: obs.ObservationID, WindowID: "123", Action: "click", X: 10, Y: 10})
	require.Equal(t, "wrong_window", apiCode(err))
	require.Zero(t, desk.clicks.Load())
}
