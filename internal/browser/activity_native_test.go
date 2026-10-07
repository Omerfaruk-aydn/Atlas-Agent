package browser

import (
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/stretchr/testify/require"
)

func TestBrowserDesktopCoordinatesRespectZoomAndDPI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		transform    browserScreenTransform
		wantX, wantY int
	}{
		{"normal", browserScreenTransform{100, 180, 1}, 125, 221},
		{"high_dpi", browserScreenTransform{200, 260, 2}, 251, 341},
		{"browser_zoom", browserScreenTransform{100, 180, 1.25}, 132, 231},
		{"left_monitor", browserScreenTransform{-1920, -900, 1.5}, -1882, -839},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := desktopBrowserEvent(activity.Event{
				ID: 7, Resource: "browser", Visible: true, Persistent: true,
				Point: true, X: 25, Y: 41, PointerRevision: 3,
				PointerX: 25.25, PointerY: 40.5, PointerKind: "aim",
			}, tc.transform, true, 0, 0, false)
			require.Equal(t, tc.wantX, e.X)
			require.Equal(t, tc.wantY, e.Y)
			require.Equal(t, float64(e.X), e.PointerX)
			require.Equal(t, float64(e.Y), e.PointerY)
			require.Equal(t, "aim", e.PointerKind)
			require.Equal(t, uint64(7), e.ID)
			require.True(t, e.Persistent)
		})
	}
}

type nativeActivityRecorder struct {
	mu     sync.Mutex
	events []activity.Event
	closed bool
	stop   func(uint64) bool
}

func (r *nativeActivityRecorder) Render(e activity.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *nativeActivityRecorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}

func (r *nativeActivityRecorder) SetStopHandler(stop func(uint64) bool) { r.stop = stop }
