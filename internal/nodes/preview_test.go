package nodes

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

type previewEntryContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *previewEntryContext) Err() error {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Err()
}

func TestPreviewCancellationDoesNotQueueBehindDesktopInput(t *testing.T) {
	h := newHarness(t, []Capability{CapDesktop}, &fakeDesktop{size: computer.Size{Width: 8, Height: 8}})
	s := h.server
	s.desktopMu.Lock()
	defer s.desktopMu.Unlock()
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &previewEntryContext{Context: parent, entered: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handlePreview(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, "GET", "/v1/node/desktop/preview", nil))
	}()
	<-ctx.entered
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("canceled preview waited for the agent's input lock")
	}
}

func TestHumanPreviewDoesNotConsumeOrReplaceAgentObservation(t *testing.T) {
	desk := &fakeDesktop{size: computer.Size{Width: 800, Height: 600}}
	h := newHarness(t, []Capability{CapDesktop}, desk)
	c := h.pair(t)
	obs, err := c.Observe(t.Context(), true)
	require.NoError(t, err)
	for range 3 {
		preview, err := c.Preview(t.Context())
		require.NoError(t, err)
		require.Empty(t, preview.ObservationID)
		require.NotEmpty(t, preview.PNG)
		require.Equal(t, obs.SessionID, preview.SessionID)
	}
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "preview-click-001", ObservationID: obs.ObservationID, Action: "click", X: 20, Y: 20})
	require.NoError(t, err, "human watching did not stale the agent's observed target")
	require.EqualValues(t, 1, desk.clicks.Load())
	_, err = c.Preview(t.Context())
	require.NoError(t, err)
	_, err = c.Input(t.Context(), DesktopInput{OperationID: "preview-click-002", ObservationID: obs.ObservationID, Action: "click", X: 20, Y: 20})
	require.Equal(t, "stale_observation", apiCode(err), "preview must not re-arm a consumed input grant")
	require.EqualValues(t, 1, desk.clicks.Load())
}

func TestPreviewRequiresScreenGrant(t *testing.T) {
	h := newHarness(t, []Capability{CapExec}, &fakeDesktop{size: computer.Size{Width: 8, Height: 8}})
	_, err := h.pair(t).Preview(t.Context())
	require.Error(t, err)
}
