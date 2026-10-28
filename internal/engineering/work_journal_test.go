package engineering

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func finishCheck(t *testing.T, s *Store, call, tool string, passed bool) string {
	t.Helper()
	ctx := t.Context()
	id, err := s.Begin(ctx, "s", call, tool, "{}", "")
	require.NoError(t, err)
	require.NoError(t, s.FinishObserved(ctx, "s", id, passed, false, Hash(call), true))
	return id
}

// A long experiment pushes the early check out of the 256-operation working
// set. Its evidence is still in the durable journal and must still count.
func TestEvidenceOutlivesTheRollingOperationWindow(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	ctx := t.Context()
	firstID := finishCheck(t, s, "early-test", "test_run", true)
	for i := range 300 {
		finishCheck(t, s, fmt.Sprintf("noise-%d", i), "view", true)
	}
	state, err := s.Read(ctx, "s")
	require.NoError(t, err)
	_, rolling := findOperation(state, firstID)
	require.False(t, rolling, "the early operation must have left the rolling window")

	now := time.UnixMilli(1000)
	_, err = s.UpdateWork(ctx, "s", func(w *WorkState, st State) error {
		turn := w.RecordUserTurn("the parser must accept empty input", now)
		item, err := w.AddItem("behavior", "parser accepts empty input", OriginUser, []int{turn.Seq}, now)
		if err != nil {
			return err
		}
		// Both the runtime ID and the call ID the model saw resolve.
		for _, ref := range []string{firstID, "early-test"} {
			if err := w.Advance(st, item.ID, ItemVerified, []string{ref}, now); err != nil {
				return fmt.Errorf("%s: %w", ref, err)
			}
		}
		return nil
	})
	require.NoError(t, err)
}

func TestJournaledEvidenceStillHonorsSourceGeneration(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	ctx := t.Context()
	finishCheck(t, s, "old-test", "test_run", true)
	require.NoError(t, s.Update(ctx, "s", func(st *State) error { st.Generation++; return nil }))
	for i := range 300 {
		finishCheck(t, s, fmt.Sprintf("noise-%d", i), "view", true)
	}

	now := time.UnixMilli(1000)
	_, err := s.UpdateWork(ctx, "s", func(w *WorkState, st State) error {
		turn := w.RecordUserTurn("fix it", now)
		item, _ := w.AddItem("behavior", "it works", OriginUser, []int{turn.Seq}, now)
		return w.Advance(st, item.ID, ItemVerified, []string{"old-test"}, now)
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "current source generation"), "an archived check from before a change is still stale: %v", err)
}

func TestJournalNeverResolvesAnotherSessionsOperation(t *testing.T) {
	t.Parallel()

	s := NewStore(t.TempDir())
	ctx := t.Context()
	id := finishCheck(t, s, "mine", "test_run", true)
	_, ok := s.JournaledOperation(ctx, "s", id)
	require.True(t, ok)
	_, ok = s.JournaledOperation(ctx, "other", id)
	require.False(t, ok)
	_, ok = s.JournaledOperation(ctx, "s", "never-ran")
	require.False(t, ok)
}
