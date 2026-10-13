package support

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLiveViewIsBoundedAndSessionScoped(t *testing.T) {
	t.Parallel()
	c := &Controller{remote: &Remote{SessionInfo: SessionInfo{SessionID: "ss-1"}}}
	for i := range 80 {
		c.live.record(Event{SessionID: "ss-1", Detail: fmt.Sprint(i)})
	}
	got := c.Timeline(0)
	require.Len(t, got, timelineCap, "bounded")
	require.Equal(t, "79", got[len(got)-1].Detail)
	require.Len(t, c.Timeline(8), 8)

	c.SetFrame(Frame{PNG: []byte{1}, SessionID: "ss-other"})
	_, ok := c.LatestFrame()
	require.False(t, ok, "another session's frame is ignored")
	c.SetFrame(Frame{PNG: make([]byte, maxFrameBytes+1), SessionID: "ss-1"})
	_, ok = c.LatestFrame()
	require.False(t, ok, "oversized frames are ignored")

	c.SetFrame(Frame{PNG: []byte{1, 2}, SessionID: "ss-1", CapturedAt: time.Now().Add(-time.Minute).UnixMilli()})
	f, ok := c.LatestFrame()
	require.True(t, ok)
	require.True(t, f.Stale(time.Now()))

	// A new session hides the previous session's frame and timeline.
	c.remote = &Remote{SessionInfo: SessionInfo{SessionID: "ss-2"}}
	_, ok = c.LatestFrame()
	require.False(t, ok)
	require.Empty(t, c.Timeline(0))
}
