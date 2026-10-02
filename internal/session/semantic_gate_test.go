package session

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestEditPlanUnresolvedJournalBlocksCompletion(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	previous := Session{ID: "session", Todos: []Todo{{ID: "task", Content: "Work", Status: TodoStatusPending}}}
	next := previous
	next.Todos = []Todo{{ID: "task", Content: "Work", Status: TodoStatusCompleted, Verification: "passed", Evidence: []TodoEvidence{{Kind: "command", Detail: "Fixture"}}}}
	for _, status := range []string{"applying", "recovering", "conflicted", "unknown-status"} {
		require.NoError(t, store.Update(t.Context(), next.ID, func(state *engineering.State) error {
			state.SemanticEdits = map[string]engineering.SemanticEditState{"edit": {ID: "edit", Status: status}}
			return nil
		}))
		require.ErrorContains(t, EngineeringCompletionGate(store)(t.Context(), previous, next), "semantic edit gate")
	}
	require.NoError(t, store.Update(t.Context(), next.ID, func(state *engineering.State) error {
		state.SemanticEdits["edit"] = engineering.SemanticEditState{ID: "edit", Status: "rolled_back"}
		return nil
	}))
	require.NoError(t, EngineeringCompletionGate(store)(t.Context(), previous, next))
}
