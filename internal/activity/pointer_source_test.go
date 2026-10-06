package activity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPhysicalMouseRemainsVisibleDuringAgentWaits(t *testing.T) {
	t.Parallel()
	var source pointerInputSource
	now := time.Now()
	e := Event{Visible: true, Persistent: true, Point: true, PointerKind: "aim", PointerAt: now}
	require.False(t, source.physicalOwns(e, 100, 100, true))
	require.True(t, source.physicalOwns(e, 120, 130, true))
	require.True(t, source.physicalOwns(e, 120, 130, true), "A stationary user pointer must not jump back to the stale agent target")
	require.True(t, source.physicalOwns(Event{Point: true}, 120, 130, true), "A new observation alone does not steal visual ownership")
	e.PointerAt = now.Add(time.Millisecond)
	require.False(t, source.physicalOwns(e, 120, 130, true), "A fresh agent target resumes the agent animation")
	require.True(t, source.physicalOwns(e, 125, 130, true))
}
