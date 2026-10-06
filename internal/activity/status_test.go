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

func TestStatusCaptionsNeverClaimUnrecordedOutcomes(t *testing.T) {
	outcomes := map[string]bool{"Done": true, "Finished": true, "All done": true}
	progress := map[string]bool{"Continuing": true, "Resuming the task": true, "Picking up where it left off": true}
	for category, phrases := range statusCatalog {
		for _, phrase := range phrases {
			if outcomes[phrase.text] {
				require.Equal(t, statusDone, category, "%q requires a completed run", phrase.text)
			}
			if progress[phrase.text] {
				require.Equal(t, statusResuming, category, "%q requires an accepted decision", phrase.text)
			}
		}
	}
	require.NotEqual(t, statusResuming, statusCategoryFor(Event{State: StateDenied}))
}
