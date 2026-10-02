package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestContractCompletionRejectsUnobservedEvidence(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	_, err := engineering.Git(ctx, root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("package api"), 0o600))
	s := engineering.NewStore(t.TempDir())
	sources, err := engineering.CaptureSources(ctx, root, []string{"api.go"})
	require.NoError(t, err)
	contract := engineering.ContractRevision{ID: "api", Root: root, OwnerTaskID: "api", Description: "API", Invariants: "Stable response", Revision: 1, Sources: sources, Checks: []engineering.ContractCheck{{Name: "check", Tool: "bash", InputJSON: `{"command":"true"}`}}}
	ref, err := s.SaveContract(ctx, contract, 0)
	require.NoError(t, err)
	previous := Session{ID: "session", Todos: []Todo{{ID: "api", Content: "Implement", Status: TodoStatusInProgress}}}
	next := previous
	next.Todos = append([]Todo{}, previous.Todos...)
	next.Todos[0].Status = TodoStatusCompleted
	require.NoError(t, s.Update(ctx, previous.ID, func(st *engineering.State) error {
		st.ContractRoot = root
		st.TaskContractRefs = map[string][]engineering.Record{"api": {ref}}
		return nil
	}))
	gate := EngineeringCompletionGate(s)
	require.ErrorContains(t, gate(ctx, previous, next), "contract gate")
	// Cost/accounting saves do not reinterpret historical completion as new work.
	next.Cost = 0.1
	require.NoError(t, gate(ctx, next, next))
}
