package engineering

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepairBoundsAndEvidence(t *testing.T) {
	t.Parallel()
	state := State{Operations: []Operation{{ID: "failed", TaskID: "task", Tool: "bash", Status: "failed", EvidenceHash: Hash("error"), OutcomeObserved: true}}, RoleExecutions: map[string]RoleExecution{"task": {TaskFingerprint: Hash("spec")}}}
	valid := RepairCase{ID: Hash("case"), TaskID: "task", TaskFingerprint: Hash("spec"), FailedOperationID: "failed", Status: "blocked", Attempts: []RepairAttempt{{ID: "attempt", Hypothesis: "Inspect the observed failure", EvidenceHash: Hash("error"), SourceFingerprint: Hash("source")}}}
	require.NoError(t, ValidateRepair(t.Context(), state, valid))
	for _, bad := range []RepairCase{
		{ID: valid.ID, TaskID: valid.TaskID, TaskFingerprint: valid.TaskFingerprint, FailedOperationID: "invented", Status: "blocked"},
		{ID: valid.ID, TaskID: valid.TaskID, TaskFingerprint: valid.TaskFingerprint, FailedOperationID: "failed", Status: "blocked", Attempts: []RepairAttempt{valid.Attempts[0], valid.Attempts[0]}},
		{ID: valid.ID, TaskID: valid.TaskID, TaskFingerprint: valid.TaskFingerprint, FailedOperationID: "failed", Status: "blocked", Attempts: []RepairAttempt{{ID: "attempt", Hypothesis: "Inspect", EvidenceHash: Hash("error"), SourceFingerprint: Hash("source"), DiagnosticRunIDs: []string{"a", "b", "c"}}}},
	} {
		require.Error(t, ValidateRepair(t.Context(), state, bad))
	}
	valid.Attempts = append(valid.Attempts, RepairAttempt{ID: "second", Hypothesis: "Changed hypothesis", EvidenceHash: Hash("error"), SourceFingerprint: Hash("source")})
	require.Error(t, ValidateRepair(t.Context(), state, valid), "Changing IDs cannot reset an unchanged evidence/source attempt")
	valid.Attempts = []RepairAttempt{}
	for _, id := range []string{"one", "two", "three", "four"} {
		valid.Attempts = append(valid.Attempts, RepairAttempt{ID: id, Hypothesis: "Inspect changed source", EvidenceHash: Hash("error"), SourceFingerprint: Hash(id)})
	}
	require.Error(t, ValidateRepair(t.Context(), state, valid), "A fourth attempt is rejected even with changed source")
}

func TestRepairObservedSuccessOnly(t *testing.T) {
	t.Parallel()
	state := State{Operations: []Operation{{ID: "failed", TaskID: "task", Tool: "bash", Status: "failed", EvidenceHash: Hash("error"), OutcomeObserved: true}}, RoleExecutions: map[string]RoleExecution{"task": {TaskFingerprint: Hash("spec")}}}
	repair := RepairCase{ID: Hash("case"), TaskID: "task", TaskFingerprint: Hash("spec"), FailedOperationID: "failed", Status: "resolved", Attempts: []RepairAttempt{{ID: "attempt", Hypothesis: "Fixed", EvidenceHash: Hash("error"), SourceFingerprint: Hash("source"), VerificationRunID: "invented"}}}
	require.Error(t, ValidateRepair(t.Context(), state, repair))
	state.Operations[0].OutcomeObserved = false
	repair.Status = "blocked"
	require.Error(t, ValidateRepair(t.Context(), state, repair), "Unavailable or denied commands are not code-failure evidence")
}
