package engineering

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerificationJournalSurvivesRollingChecksAndOperations(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	ctx := t.Context()
	var originalOperation string
	for run := range 8 {
		call := fmt.Sprintf("run-%d", run)
		op, err := s.Begin(ctx, "session", call, "verify", `{}`, "task")
		require.NoError(t, err)
		if run == 0 {
			originalOperation = op
		}
		require.NoError(t, s.Update(ctx, "session", func(st *State) error {
			for check := range 12 {
				st.Checks = append(st.Checks, Check{RunID: call, TaskID: "task", Name: fmt.Sprintf("check-%d", check), Tool: "bash", Passed: true, Evidence: "Observed exit 0", InputHash: Hash("input")})
			}
			return nil
		}))
		require.NoError(t, s.FinishObserved(ctx, "session", op, true, false, Hash(call), true))
	}
	st, err := s.Read(ctx, "session")
	require.NoError(t, err)
	require.Len(t, st.Checks, 64)
	op, checks, err := s.VerificationObservation(ctx, "session", "run-0", "task", st)
	require.NoError(t, err)
	require.Equal(t, originalOperation, op.ID)
	require.Len(t, checks, 12)
	require.NoError(t, s.Update(ctx, "session", func(st *State) error { st.Operations = nil; st.Checks = nil; return nil }))
	st, err = s.Read(ctx, "session")
	require.NoError(t, err)
	_, checks, err = NewStore(t.TempDir()).VerificationObservation(ctx, "session", "run-0", "task", st)
	require.NoError(t, err)
	require.Empty(t, checks, "Foreign store cannot invent proof")
	_, checks, err = s.VerificationObservation(ctx, "session", "run-0", "task", st)
	require.NoError(t, err)
	require.Len(t, checks, 12)
	_, checks, err = s.VerificationObservation(ctx, "session", "run-0", "other-task", st)
	require.NoError(t, err)
	require.Empty(t, checks)
}

func TestContractCertificationWithMoreThanRecentCheckLimit(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	root := t.TempDir()
	_, err := Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.txt"), []byte("fixed"), 0o600))
	sources, err := CaptureSources(t.Context(), root, []string{"api.txt"})
	require.NoError(t, err)
	fp, err := SourceFingerprint(t.Context(), root, s.Dir())
	require.NoError(t, err)
	var refs []Record
	for index := range 8 {
		contract := ContractRevision{ID: fmt.Sprintf("contract-%d", index), Root: root, OwnerTaskID: "task", Description: "API", Invariants: "Fixed value", Revision: 1, Sources: sources}
		for check := range 12 {
			contract.Checks = append(contract.Checks, ContractCheck{Name: fmt.Sprintf("check-%d", check), Tool: "bash", InputJSON: `{}`})
		}
		ref, err := s.SaveContract(t.Context(), contract, 0)
		require.NoError(t, err)
		refs = append(refs, ref)
		call := fmt.Sprintf("contract-run-%d", index)
		op, err := s.Begin(t.Context(), "session", call, "verify", `{}`, "task")
		require.NoError(t, err)
		for _, check := range contract.Checks {
			require.NoError(t, s.AppendVerificationCheck(t.Context(), "session", Check{RunID: call, TaskID: "task", ContractHash: ref.Ref.Hash, Name: check.Name, Tool: "bash", Passed: true, Evidence: "Observed exit 0", InputHash: Hash(check.InputJSON)}))
		}
		require.NoError(t, s.FinishObserved(t.Context(), "session", op, true, false, Hash(call), true))
		require.NoError(t, s.SaveContractEvidence(t.Context(), "session", contract.ID, ContractEvidence{Root: root, TaskID: "task", TaskFingerprint: Hash("spec"), Contract: ref, RunID: call, SourceFingerprint: fp}))
	}
	st, err := s.Read(t.Context(), "session")
	require.NoError(t, err)
	require.Len(t, st.Checks, 64)
	require.NoError(t, s.ValidateTaskContracts(t.Context(), "session", root, "task", Hash("spec"), refs))
	// Remove only recent views; immutable machine journals remain authoritative.
	require.NoError(t, s.Update(t.Context(), "session", func(st *State) error { st.Checks = nil; st.Operations = nil; return nil }))
	require.NoError(t, s.ValidateTaskContracts(t.Context(), "session", root, "task", Hash("spec"), refs))
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.txt"), []byte("changed"), 0o600))
	require.Error(t, s.ValidateTaskContracts(t.Context(), "session", root, "task", Hash("spec"), refs))
}

func TestRepairJournalsRetainOriginalFailureAndIndependentReview(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	ctx := t.Context()
	spec, source := Hash("spec"), Hash("source")
	require.NoError(t, s.Update(ctx, "session", func(st *State) error {
		st.RoleExecutions["task"] = RoleExecution{TaskID: "task", TaskFingerprint: spec}
		return nil
	}))
	finish := func(call, tool, role string, passed bool) string {
		op, err := s.Begin(ctx, "session", call, tool, `{}`, "task")
		require.NoError(t, err)
		if role != "" {
			require.NoError(t, s.Update(ctx, "session", func(st *State) error {
				for i := range st.Operations {
					if st.Operations[i].ID == op {
						st.Operations[i].AgentName = role
					}
				}
				return nil
			}))
		}
		if tool == "verify" && passed {
			require.NoError(t, s.AppendVerificationCheck(ctx, "session", Check{TaskID: "task", RunID: call, Name: "check", Tool: "bash", Passed: true, Evidence: "Actual exit 0"}))
		}
		require.NoError(t, s.FinishObserved(ctx, "session", op, passed, false, Hash(call), true))
		return op
	}
	failure := finish("original", "bash", "", false)
	diagnostic := finish("diagnostic", "verify", "", false)
	debug := finish("debug", "agent", "debug", true)
	verify := finish("verify", "verify", "", true)
	review := finish("review", "agent", "review", true)
	r := RepairCase{ID: Hash("case"), TaskID: "task", TaskFingerprint: spec, FailedOperationID: failure, Status: "resolved", Attempts: []RepairAttempt{{ID: "attempt", Hypothesis: "Fix actual failure", EvidenceHash: Hash("original"), SourceFingerprint: source, DiagnosticRunIDs: []string{diagnostic}, ImplementationRunID: debug, VerificationRunID: verify, ReviewRunID: review, ReviewPassed: true, VerifiedSourceFingerprint: source, ReviewedSourceFingerprint: source}}}
	require.NoError(t, s.Update(ctx, "session", func(st *State) error { st.Operations = nil; st.Checks = nil; return nil }))
	st, err := s.Read(ctx, "session")
	require.NoError(t, err)
	require.Error(t, ValidateRepair(ctx, st, r))
	require.NoError(t, s.ValidateRepair(ctx, "session", st, r))
	require.Error(t, s.ValidateRepair(ctx, "other-session", st, r))
}
