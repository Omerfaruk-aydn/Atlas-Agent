package dialog

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/stretchr/testify/require"
)

func connectedSupport(t *testing.T, lang string) *Support {
	t.Helper()
	svc := support.NewService(t.TempDir(), func() support.ServiceConfig {
		return support.ServiceConfig{Name: "Ofis-PC", DiscoveryPort: -1, LANListen: "127.0.0.1:0", Overlay: quietOverlay{}}
	}, nil)
	t.Cleanup(svc.Close)
	d := NewSupport(supportCommon(lang), svc)
	d.view = supportConnect
	d.hasCtl = true
	d.ctl = support.ControllerStatus{State: support.StateConnected, Remote: &support.Remote{SessionInfo: support.SessionInfo{RemoteName: "Omerfaruk", RemoteDeviceID: "node-e56a", SessionID: "ss-d70f", Generation: 1, OS: "windows", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}}}
	return d
}

func TestSupportPanelShowsLiveScreenAndTimeline(t *testing.T) {
	t.Parallel()
	d := connectedSupport(t, "en")
	out := renderSupport(d)
	require.Contains(t, out, "Remote screen")
	require.Contains(t, out, "No screen captured yet")
	require.Contains(t, out, "No operations yet.")

	now := time.Now()
	d.hasFrame = true
	d.frame = support.Frame{PNG: []byte{1}, SessionID: "ss-d70f", Generation: 1, Width: 1920, Height: 1080, CapturedAt: now.Add(-5 * time.Second).UnixMilli()}
	d.timeline = []support.Event{
		{At: now.Add(-time.Minute).UnixMilli(), Detail: "launch: chrome", State: "ok"},
		{At: now.UnixMilli(), Detail: "exec: shutdown /s /t 0", State: "outcome_unknown", Effect: "unknown"},
	}
	out = renderSupport(d)
	require.Contains(t, out, "1920×1080")
	require.NotContains(t, out, "Stale:")
	require.Contains(t, out, "launch: chrome")
	require.Contains(t, out, "? exec: shutdown")
	require.Contains(t, out, "open screen")

	d.frame.CapturedAt = now.Add(-2 * time.Minute).UnixMilli()
	require.Contains(t, renderSupport(d), "Stale:")
}

func TestSupportPanelTurkish(t *testing.T) {
	t.Parallel()
	d := connectedSupport(t, "tr")
	out := renderSupport(d)
	require.Contains(t, out, "Uzak ekran")
	require.Contains(t, out, "Görev zaman çizelgesi")
}
