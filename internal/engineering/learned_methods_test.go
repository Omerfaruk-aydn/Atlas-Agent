package engineering

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLearnedMethodExtractionAndSourceInvalidation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	store := NewStore(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(root, "fixture.go"), []byte("package fixture\n"), 0o644))
	require.NoError(t, store.Update(ctx, "session", func(st *State) error {
		st.Generation = 2
		st.Operations = []Operation{{ID: "edit", Tool: "edit", Status: "completed", OutcomeObserved: true, Generation: 1, FinishedAt: 1}, {ID: "check", Tool: "test_run", Status: "completed", OutcomeObserved: true, Generation: 2, FinishedAt: 2}}
		return nil
	}))
	in := LearnedMethod{Name: "fixture-repair", Description: "Use for this fixture", Conditions: []string{"The fixture API matches"}, Versions: []string{"Go 1.26"}, Limits: []string{"Only validated on a local fixture"}, OperationIDs: []string{"edit", "check"}, Sources: []SourceReference{{Path: "fixture.go"}}}
	m, err := store.ExtractMethod(ctx, root, "session", in)
	require.NoError(t, err)
	require.Equal(t, "candidate", m.Status)
	require.Len(t, m.Steps, 2)
	require.Contains(t, MethodInstructions(m), "single success is not a universal guarantee")
	require.Contains(t, MethodInstructions(m), "Go 1.26")
	m2, err := store.ExtractMethod(ctx, root, "session", in)
	require.NoError(t, err)
	require.Equal(t, m.ID, m2.ID)
	methods, err := store.LearnedMethods(ctx, root)
	require.NoError(t, err)
	require.Len(t, methods, 1)
	_, err = store.CurrentMethod(ctx, root, m.ID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "fixture.go"), []byte("package changed\n"), 0o644))
	_, err = store.CurrentMethod(ctx, root, m.ID)
	require.ErrorContains(t, err, "source changed")
}

func TestLearnedMethodRejectsUnobservedOrUnverifiedSequences(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	root := t.TempDir()
	require.NoError(t, store.Update(t.Context(), "s", func(st *State) error {
		st.Operations = []Operation{{ID: "a", Tool: "view", Status: "completed", OutcomeObserved: true, FinishedAt: 1}, {ID: "b", Tool: "bash", Status: "completed", OutcomeObserved: true, FinishedAt: 2}}
		return nil
	}))
	in := LearnedMethod{Name: "bad-method", Description: "Test", Conditions: []string{"Local"}, Versions: []string{"Unknown"}, Limits: []string{"Limited"}, OperationIDs: []string{"a", "b"}}
	_, err := store.ExtractMethod(t.Context(), root, "s", in)
	require.ErrorContains(t, err, "verification")
	in.OperationIDs = []string{"a", "invented"}
	_, err = store.ExtractMethod(t.Context(), root, "s", in)
	require.ErrorContains(t, err, "observed")
}
