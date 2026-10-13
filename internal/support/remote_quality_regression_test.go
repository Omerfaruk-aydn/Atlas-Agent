package support

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRemoteQualityLatestFrameRejectsEarlierGeneration(t *testing.T) {
	c := &Controller{remote: &Remote{SessionInfo: SessionInfo{SessionID: "ss", Generation: 2}}}
	c.SetFrame(Frame{PNG: []byte{1}, SessionID: "ss", Generation: 1})
	_, ok := c.LatestFrame()
	require.False(t, ok, "generation 1 frame must not be used for generation 2")
}

func TestRemoteQualityCachedFrameNotLiveWhenOffline(t *testing.T) {
	c := &Controller{remote: &Remote{SessionInfo: SessionInfo{SessionID: "ss", Generation: 1}}}
	c.SetFrame(Frame{PNG: []byte{1}, SessionID: "ss", Generation: 1, CapturedAt: time.Now().UnixMilli()})
	c.machine = &Machine{state: StateReconnecting}
	f, ok := c.LatestFrame()
	require.True(t, !ok || f.Stale(time.Now()), "disconnect must immediately label a cached frame stale")
}
