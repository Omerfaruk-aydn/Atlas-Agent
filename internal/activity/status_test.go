package activity

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
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

func TestStatusCatalogIsBroadAndTranslated(t *testing.T) {
	total := 0
	for _, phrases := range statusCatalog {
		total += len(phrases)
		for _, phrase := range phrases {
			require.Positive(t, phrase.weight)
			for _, language := range i18n.Languages() {
				if language.Code != "en" {
					require.True(t, i18n.Has(language.Code, phrase.text), "%s lacks %q", language.Code, phrase.text)
				}
			}
		}
	}
	require.GreaterOrEqual(t, total, 60)
}

func TestStatusPickerHoldsAndNeverRepeatsBackToBack(t *testing.T) {
	var p statusPicker
	now := time.Unix(1000, 0)
	e := Event{State: StateThinking, Language: "en"}
	first := p.text(e, now)
	require.Equal(t, "Thinking", first, "A new category starts plainly")
	require.Equal(t, first, p.text(e, now.Add(time.Second)), "Captions do not flicker")
	previous := first
	for i := 1; i <= 50; i++ {
		next := p.text(e, now.Add(time.Duration(i)*statusDwell))
		require.NotEqual(t, previous, next)
		previous = next
	}
	// A state change updates immediately.
	require.Equal(t, "Waiting for your answer", p.text(Event{State: StateAwaitQuestion, Language: "en"}, now.Add(51*statusDwell+time.Millisecond)))
}

func TestFormatElapsed(t *testing.T) {
	require.Equal(t, "00:12", FormatElapsed(12*time.Second))
	require.Equal(t, "01:48", FormatElapsed(108*time.Second+400*time.Millisecond))
	require.Equal(t, "1:02:03", FormatElapsed(time.Hour+2*time.Minute+3*time.Second))
	require.Equal(t, "00:00", FormatElapsed(-time.Second))
}
