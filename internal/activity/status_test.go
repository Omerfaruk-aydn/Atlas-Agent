package activity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusCaptionsMatchRecordedEvents(t *testing.T) {
	for _, tc := range []struct {
		event Event
		want  statusCategory
	}{
		{Event{State: StateThinking}, statusThinking},
		{Event{State: StateTool, Action: "navigate"}, statusOpening},
		{Event{State: StateTool, Action: "type"}, statusTyping},
		{Event{State: StateTool, Action: "click"}, statusClicking},
		{Event{State: StateTool, Action: "upload"}, statusUploading},
		{Event{State: StateTool, Action: "screenshot", AfterFailure: true}, statusObserving},
		{Event{State: StateAwaitQuestion, Action: "click"}, statusAwaitQuestion},
		{Event{State: StateAwaitPermission}, statusAwaitPermission},
		{Event{State: StateDenied}, statusDenied},
		{Event{State: StateFailed}, statusFailed},
		{Event{State: StateDone}, statusDone},
	} {
		require.Equal(t, tc.want, statusCategoryFor(tc.event), tc.event)
	}
}
