package session

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestDeliveryGateRejectsFutureStagesThroughSessionSaveBoundary(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	first := Todo{ID: "first", Content: "First", Status: TodoStatusPending}
	second := Todo{ID: "second", Content: "Second", Status: TodoStatusPending}
	plan := engineering.DeliveryPlan{Root: t.TempDir(), Profile: "feature", Requirements: []engineering.Requirement{{ID: "feature", Description: "Feature", TaskIDs: []string{"first", "second"}}}, Stages: []engineering.Stage{{ID: "first", Title: "First", TaskIDs: []string{"first"}}, {ID: "second", Title: "Second", TaskIDs: []string{"second"}}}, TaskFingerprints: map[string]string{"first": TaskFingerprint(first), "second": TaskFingerprint(second)}}
	require.NoError(t, store.Update(t.Context(), "session", func(st *engineering.State) error { st.Delivery = &plan; return nil }))
	previous := Session{ID: "session", Todos: []Todo{first, second}}
	next := Session{ID: "session", Todos: []Todo{first, second}}
	next.Todos[1].Status = TodoStatusInProgress
	// The gate is tied to the persisted store, including after recreation.
	gate := EngineeringCompletionGate(engineering.NewStore(store.Dir() + "/.."))
	require.ErrorContains(t, gate(t.Context(), previous, next), "current delivery stage")
	next.Todos[1].Status = TodoStatusPending
	next.Todos[0].Status = TodoStatusInProgress
	require.NoError(t, gate(t.Context(), previous, next))
	next.Todos[0].Content = "Changed scope"
	require.ErrorContains(t, gate(t.Context(), previous, next), "changed")
}
