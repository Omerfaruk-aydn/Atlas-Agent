package engineering

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUnrelatedLintCannotVerifyBehaviorRequirement(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(5000)
	var w WorkState
	turn := w.RecordUserTurn("users must not read each other's invoices", now)
	access, err := w.AddItem("behavior", "cross-user invoice access is rejected", OriginUser, []int{turn.Seq}, now)
	require.NoError(t, err)
	docs, err := w.AddItem("deliverable", "the change is formatted", OriginUser, []int{turn.Seq}, now)
	require.NoError(t, err)

	st := State{Generation: 1, Operations: []Operation{
		op("lint", "lint_run", "completed", 1, 100),
	}}
	require.ErrorContains(t, w.Advance(st, access.ID, ItemVerified, []string{"lint"}, now), "static check")
	require.Equal(t, ItemOpen, w.item(access.ID).Status, "a rejected claim must not change the item")
	_, err = w.LatestCheckFor(st, access.ID)
	require.ErrorContains(t, err, "static check", "the automatic latest-check shortcut must apply the same rule")

	// The same evidence is acceptable for a claim a linter can actually support.
	require.NoError(t, w.Advance(st, docs.ID, ItemVerified, []string{"lint"}, now))

	// A behavior check exercises the behavior and may sit behind a newer lint.
	st.Operations = append([]Operation{op("test", "test_run", "completed", 1, 110)}, append(st.Operations, op("lint2", "lint_run", "completed", 1, 120))...)
	latest, err := w.LatestCheckFor(st, access.ID)
	require.NoError(t, err)
	require.Equal(t, "test", latest.ID)
	require.NoError(t, w.Advance(st, access.ID, ItemVerified, []string{latest.ID}, now))
}

func TestAcceptanceCheckPinsTheVerifyingTool(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(5000)
	var w WorkState
	turn := w.RecordUserTurn("checkout must work in the browser", now)
	item, err := w.AddItem("behavior", "checkout completes", OriginUser, []int{turn.Seq}, now)
	require.NoError(t, err)
	w.item(item.ID).AcceptanceCheck = "ui_verify"

	st := State{Generation: 1, Operations: []Operation{
		op("unit", "test_run", "completed", 1, 100),
	}}
	require.ErrorContains(t, w.Advance(st, item.ID, ItemVerified, []string{"unit"}, now), "acceptance check")
	st.Operations = append(st.Operations, op("ui", "ui_verify", "completed", 1, 110))
	require.NoError(t, w.Advance(st, item.ID, ItemVerified, []string{"ui"}, now))
}

func TestClosedItemsReturnCapacityAndStayReferenceable(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(1000)
	var w WorkState
	w.RecordUserTurn("do the work", now)
	var first string
	for i := range maxWorkItems {
		item, err := w.AddItem("deliverable", "idea", OriginProposal, nil, now)
		require.NoError(t, err)
		if i == 0 {
			first = item.ID
		}
		require.NoError(t, w.Decline(item.ID, now))
	}
	// Before the fix every declined item still counted and this failed.
	kept, err := w.AddItem("behavior", "real requirement", OriginAssumption, nil, now)
	require.NoError(t, err)
	require.Len(t, w.Items, 1)
	require.Equal(t, kept.ID, w.Items[0].ID)
	archived, ok := w.ArchivedItem(first)
	require.True(t, ok)
	require.Equal(t, ItemDeclined, archived.Status)
	require.ErrorContains(t, w.Advance(State{}, first, ItemImplemented, []string{"x"}, now), "archived")

	// A long task cycles through far more items than either bound.
	for range 5 * maxWorkItems {
		item, err := w.AddItem("deliverable", "more", OriginProposal, nil, now)
		require.NoError(t, err)
		require.NoError(t, w.Decline(item.ID, now))
	}
	require.LessOrEqual(t, len(w.Items), maxWorkItems)
	require.LessOrEqual(t, len(w.Archive), maxWorkArchive)

	// Open work is never archived: a full set of live items still refuses.
	var live WorkState
	for range maxWorkItems {
		_, err := live.AddItem("deliverable", "open", OriginProposal, nil, now)
		require.NoError(t, err)
	}
	_, err = live.AddItem("deliverable", "one too many", OriginProposal, nil, now)
	require.ErrorContains(t, err, "open contract items")
}
