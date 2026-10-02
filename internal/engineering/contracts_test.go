package engineering

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractCASAndConsumers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	s := NewStore(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("package api"), 0o600))
	sources, err := CaptureSources(ctx, root, []string{"api.go"})
	require.NoError(t, err)
	c := ContractRevision{ID: "api", Root: root, OwnerTaskID: "owner", Description: "Public API", Invariants: "Stable return value", Revision: 1, ConsumerTaskIDs: []string{"a", "b"}, Sources: sources, Checks: []ContractCheck{{Name: "api-test", Tool: "bash", InputJSON: `{"command":"go test ./..."}`}}}
	r1, err := s.SaveContract(ctx, c, 0)
	require.NoError(t, err)
	for _, id := range []string{"owner", "a", "b"} {
		require.NoError(t, s.ValidateContractRefs(ctx, root, id, []Record{r1}))
	}
	c.Revision = 2
	c.Invariants = "Stable return value and error"
	r2, err := s.SaveContract(ctx, c, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(2), r2.Revision)
	for _, id := range []string{"a", "b"} {
		require.Error(t, s.ValidateContractRefs(ctx, root, id, []Record{r1}))
		require.NoError(t, s.ValidateContractRefs(ctx, root, id, []Record{r2}))
	}
	_, err = s.SaveContract(ctx, c, 1)
	require.Error(t, err, "identical revision retries must not become new approvals")
	c.Revision = 3
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, e := s.SaveContract(ctx, c, 2); errs <- e })
	}
	wg.Wait()
	close(errs)
	successes := 0
	for e := range errs {
		if e == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	contracts, err := s.ContractsForTask(ctx, root, "a")
	require.NoError(t, err)
	require.Equal(t, uint64(3), contracts[0].Revision)
}

func TestContractSourcesAndObservedChecks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	_, err := Git(ctx, root, "init")
	require.NoError(t, err)
	s := NewStore(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("package api"), 0o600))
	sources, err := CaptureSources(ctx, root, []string{"api.go"})
	require.NoError(t, err)
	c := ContractRevision{ID: "api", Root: root, OwnerTaskID: "a", Description: "API", Invariants: "Returns data", Revision: 1, Sources: sources, Checks: []ContractCheck{{Name: "check", Tool: "bash", InputJSON: `{"command":"true"}`}}}
	bad := c
	bad.Sources = []SourceReference{{Path: "../outside", Fingerprint: Hash("fake")}}
	_, err = s.SaveContract(ctx, bad, 0)
	require.Error(t, err)
	r, err := s.SaveContract(ctx, c, 0)
	require.NoError(t, err)
	fp, err := SourceFingerprint(ctx, root, s.Dir())
	require.NoError(t, err)
	proof := ContractEvidence{Root: root, TaskID: "a", TaskFingerprint: Hash("task"), Contract: r, RunID: "check-run", SourceFingerprint: fp}
	require.Error(t, s.SaveContractEvidence(ctx, "session", c.ID, proof))
	require.NoError(t, s.Update(ctx, "session", func(st *State) error {
		st.Operations = append(st.Operations, Operation{ID: "op", CallID: proof.RunID, Tool: "verify", TaskID: "a", Status: "completed", OutcomeObserved: true, EvidenceHash: Hash("ok")})
		st.Checks = append(st.Checks, Check{ContractHash: r.Ref.Hash, InputHash: Hash(c.Checks[0].InputJSON), TaskID: "a", RunID: proof.RunID, Name: "check", Tool: "bash", Passed: true, Evidence: "Executed"})
		return nil
	}))
	require.NoError(t, s.SaveContractEvidence(ctx, "session", c.ID, proof))
	require.NoError(t, s.ValidateTaskContracts(ctx, "session", root, "a", proof.TaskFingerprint, []Record{r}))
	require.Error(t, s.ValidateTaskContracts(ctx, "session", root, "a", Hash("changed task"), []Record{r}))
	c.Revision = 2
	c.Invariants = "Changed response"
	r2, err := s.SaveContract(ctx, c, 1)
	require.NoError(t, err)
	proof.Contract = r2
	require.Error(t, s.SaveContractEvidence(ctx, "session", c.ID, proof), "an old check cannot approve a new revision even with the same command")
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("package changed"), 0o600))
	require.Error(t, s.ValidateContractRefs(ctx, root, "a", []Record{r}))
	require.Error(t, s.ValidateTaskContracts(context.Background(), "session", root, "a", proof.TaskFingerprint, []Record{r}))
}

func TestContractHistorySurvivesArtifactCleanup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("package api"), 0o600))
	s := NewStore(t.TempDir())
	sources, err := CaptureSources(t.Context(), root, []string{"api.go"})
	require.NoError(t, err)
	c := ContractRevision{ID: "api", Root: root, OwnerTaskID: "api", Description: "API", Invariants: "Old return", Revision: 1, Sources: sources, Checks: []ContractCheck{{Name: "check", Tool: "bash", InputJSON: `{"command":"true"}`}}}
	old, err := s.SaveContract(t.Context(), c, 0)
	require.NoError(t, err)
	c.Revision, c.Invariants = 2, "New return"
	_, err = s.SaveContract(t.Context(), c, 1)
	require.NoError(t, err)
	_, err = s.CleanArtifacts(t.Context())
	require.NoError(t, err)
	_, err = s.ReadArtifact(t.Context(), old.Ref)
	require.NoError(t, err, "historical contract evidence remains reachable")
}
