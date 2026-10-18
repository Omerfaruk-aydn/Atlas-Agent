package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestWorkProjectionRefreshesWithoutChangingUserOrTools(t *testing.T) {
	t.Parallel()
	st := engineering.State{Work: &engineering.WorkState{Budget: engineering.WorkBudget{Tier: engineering.TierStandard}}}
	st.Work.RecordUserTurn("fix cache", time.Now())
	messages := []fantasy.Message{
		{Role: fantasy.MessageRoleSystem, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "policy" + workBlock(st) + "\nother policy"}}},
		fantasy.NewUserMessage("<work_state>user text must remain</work_state>"),
	}
	_, err := st.Work.AddItem("constraint", "retain public API", engineering.OriginUser, []int{1}, time.Now())
	require.NoError(t, err)
	st.Generation = 3
	updated := withCurrentWork(messages, st)
	text := updated[0].Content[0].(fantasy.TextPart).Text
	require.Contains(t, text, "retain public API")
	require.Contains(t, text, `"source_generation":3`)
	require.Contains(t, text, "other policy")
	require.Equal(t, 1, countWorkBlocks(text))
	require.Equal(t, messages[1], updated[1])
	require.NotContains(t, messages[0].Content[0].(fantasy.TextPart).Text, "retain public API")
}

func countWorkBlocks(s string) int {
	count := 0
	for i := 0; i+len("<work_state>") <= len(s); i++ {
		if s[i:i+len("<work_state>")] == "<work_state>" {
			count++
		}
	}
	return count
}

func TestContinueAfterDeliveryDoesNotEraseContract(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	_, err := recordWorkTurn(t.Context(), store, "s", "fix cache", "")
	require.NoError(t, err)
	_, err = store.UpdateWork(t.Context(), "s", func(w *engineering.WorkState, _ engineering.State) error {
		w.Items = []engineering.WorkItem{{ID: "R1", Text: "fix cache", Origin: engineering.OriginUser, Status: engineering.ItemDelivered}}
		return nil
	})
	require.NoError(t, err)
	w, err := recordWorkTurn(t.Context(), store, "s", "devam et", "")
	require.NoError(t, err)
	require.Len(t, w.Items, 1)
	w, err = recordWorkTurn(t.Context(), store, "s", "create export", "")
	require.NoError(t, err)
	require.Empty(t, w.Items)
}

func TestProjectionNeverDropsUserConstraintsToMeetSoftBudget(t *testing.T) {
	t.Parallel()
	w := &engineering.WorkState{Budget: engineering.WorkBudget{Tier: engineering.TierDeep}}
	w.RecordUserTurn("preserve public contracts", time.Now())
	for i := 0; i < 12; i++ {
		_, err := w.AddItem("constraint", strings.Repeat("preserve public API ", 40), engineering.OriginUser, []int{1}, time.Now())
		require.NoError(t, err)
	}
	block := workBlock(engineering.State{Work: w})
	require.Contains(t, block, "critical_constraints")
	require.Contains(t, block, `"id":"R12"`)
	require.Contains(t, block, "before consequential changes")
}
