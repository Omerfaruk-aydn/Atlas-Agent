package engineering

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/stretchr/testify/require"
)

func workWithUserItem(t *testing.T, status string) *WorkState {
	t.Helper()
	now := time.UnixMilli(1000)
	var w WorkState
	turn := w.RecordUserTurn("rename the helper", now)
	item, err := w.AddItem("behavior", "helper renamed everywhere", OriginUser, []int{turn.Seq}, now)
	require.NoError(t, err)
	w.item(item.ID).Status = status
	return &w
}

func TestSimpleTaskStaysOnTheShortPath(t *testing.T) {
	t.Parallel()

	b := WorkBudget{Tier: TierDirect}
	w := workWithUserItem(t, ItemOpen)
	got := RaiseForRuntimeSignals(b, w, State{Generation: 1})
	require.Equal(t, TierDirect, got.Tier, "one small change with nothing odd observed does not escalate")
	require.Empty(t, got.Reasons)
}

func TestObservedRiskRaisesTheTierWithReasons(t *testing.T) {
	t.Parallel()

	// Many changes with work still unverified.
	got := RaiseForRuntimeSignals(WorkBudget{Tier: TierDirect}, workWithUserItem(t, ItemImplemented), State{Generation: signalManyChanges})
	require.Equal(t, TierStandard, got.Tier)
	require.Contains(t, got.Reasons[0], "unverified")

	// A wide change set needs the deep path whatever the wording was.
	got = RaiseForRuntimeSignals(WorkBudget{Tier: TierStandard}, workWithUserItem(t, ItemOpen), State{Generation: signalWideChanges})
	require.Equal(t, TierDeep, got.Tier)

	// Proof that went stale after later edits.
	w := workWithUserItem(t, ItemVerified)
	w.Items[0].Evidence = []EvidenceRef{{OperationID: "x", Passed: true, Generation: 1}}
	second, err := w.AddItem("behavior", "second", OriginAssumption, nil, time.UnixMilli(1))
	require.NoError(t, err)
	_ = second
	w.Items = append(w.Items, WorkItem{ID: "R9", Kind: "behavior", Origin: OriginUser, Status: ItemVerified, Evidence: []EvidenceRef{{Passed: true, Generation: 1}}})
	got = RaiseForRuntimeSignals(WorkBudget{Tier: TierStandard}, w, State{Generation: 3})
	require.Equal(t, TierDeep, got.Tier)
	require.Contains(t, got.Reasons[0], "stale")

	// An unknown outcome is the highest-risk state.
	st := State{Generation: 1}
	st.Outcomes.Record(outcome.Record{OpID: "op", Tool: "mcp_srv_send", Phase: outcome.PhaseUnknown, Mutation: true})
	got = RaiseForRuntimeSignals(WorkBudget{Tier: TierDirect}, workWithUserItem(t, ItemOpen), st)
	require.Equal(t, TierDeep, got.Tier)
}

func TestSignalsNeverLowerTheTierOrOverrideTheUser(t *testing.T) {
	t.Parallel()

	w := workWithUserItem(t, ItemOpen)
	require.Equal(t, TierDeep, RaiseForRuntimeSignals(WorkBudget{Tier: TierDeep}, w, State{}).Tier)
	pinned := WorkBudget{Tier: TierDirect, Override: TierDirect}
	require.Equal(t, TierDirect, RaiseForRuntimeSignals(pinned, w, State{Generation: 99}).Tier, "a user override wins over every signal")
}
