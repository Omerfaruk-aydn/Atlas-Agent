package engineering

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func findingFixture(t *testing.T) (*Store, Finding) {
	t.Helper()
	root := t.TempDir()
	_, err := Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "api.go"), []byte("broken"), 0o600))
	s := NewStore(t.TempDir())
	fp, err := SourceFingerprint(t.Context(), root, s.Dir())
	require.NoError(t, err)
	f := Finding{Root: root, TaskID: "api", TaskFingerprint: Hash("task"), ReviewerExecutionID: "original-review", ImplementerExecutionID: "implementation", SourceFingerprint: fp, Status: "open", Path: "api.go", StartLine: 1, EndLine: 1, Severity: 1, Issue: "Empty response", Expected: "Return value", Evidence: "Observed empty result", Checks: []ContractCheck{{Name: "check", Tool: "bash", InputJSON: `{"command":"check"}`}}}
	f.ID = FindingID(f)
	require.NoError(t, s.Update(t.Context(), "session", func(st *State) error {
		st.Operations = []Operation{{ID: "original-review", Tool: "agent", AgentName: "review", TaskID: f.TaskID, Status: "completed"}, {ID: "implementation", Tool: "agent", AgentName: "backend", TaskID: f.TaskID, Status: "completed"}}
		return nil
	}))
	return s, f
}

func TestFindingsIdempotentTasks(t *testing.T) {
	t.Parallel()
	s, f := findingFixture(t)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, err := s.SaveFinding(t.Context(), "session", f, 0); require.NoError(t, err) })
	}
	wg.Wait()
	findings, err := s.TaskFindings(t.Context(), "session", f.TaskID)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, "repair-"+f.ID[:32], findings[0].RemediationID())
}

func TestFindingsIndependentVerification(t *testing.T) {
	t.Parallel()
	s, f := findingFixture(t)
	r, err := s.SaveFinding(t.Context(), "session", f, 0)
	require.NoError(t, err)
	f.Status = "fixed"
	f.RemediationTaskID = f.RemediationID()
	r, err = s.SaveFinding(t.Context(), "session", f, r.Revision)
	require.NoError(t, err)
	f.Status = "verified"
	f.VerificationReviewerID = f.ImplementerExecutionID
	f.VerificationRunIDs = []string{"old-check"}
	f.VerifiedSourceFingerprint = f.SourceFingerprint
	f.VerifiedTaskFingerprint = f.TaskFingerprint
	_, err = s.SaveFinding(t.Context(), "session", f, r.Revision)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.Root, "api.go"), []byte("fixed"), 0o600))
	f.VerifiedSourceFingerprint, err = SourceFingerprint(t.Context(), f.Root, s.Dir())
	require.NoError(t, err)
	f.VerificationReviewerID = "fresh-review"
	f.VerificationRunIDs = []string{"new-check"}
	require.NoError(t, s.Update(t.Context(), "session", func(st *State) error {
		st.Operations = append(st.Operations, Operation{ID: "fresh-review", Tool: "agent", AgentName: "review", TaskID: f.TaskID, Status: "completed"}, Operation{ID: "verify", CallID: "new-check", Tool: "verify", TaskID: f.TaskID, Status: "completed", OutcomeObserved: true, EvidenceHash: Hash("passed")})
		st.Checks = append(st.Checks, Check{Name: "check", Tool: "bash", InputHash: Hash(f.Checks[0].InputJSON), FindingID: f.ID, FindingReview: f.VerificationBinding(), TaskID: f.TaskID, RunID: "new-check", Passed: true, Evidence: "Observed exit 0"})
		return nil
	}))
	_, err = s.SaveFinding(t.Context(), "session", f, r.Revision)
	require.NoError(t, err)
	require.False(t, s.FindingBlocks(t.Context(), f, f.VerifiedSourceFingerprint))
	require.True(t, s.FindingBlocks(t.Context(), f, Hash("different source")))
	// A fresh review can rediscover an apparently resolved defect. Its existing
	// identity must reopen, rather than silently preserve the old certification.
	reported := f
	reported.Status = "open"
	reported.SourceFingerprint = f.VerifiedSourceFingerprint
	reported.ReviewerExecutionID = "rediscovered-review"
	require.NoError(t, s.Update(t.Context(), "session", func(st *State) error {
		st.Operations = append(st.Operations, Operation{ID: reported.ReviewerExecutionID, Tool: "agent", AgentName: "review", TaskID: f.TaskID, Status: "completed"})
		return nil
	}))
	_, err = s.SaveFinding(t.Context(), "session", reported, 0)
	require.NoError(t, err)
	current, _, err := s.ReadFinding(t.Context(), "session", f.ID)
	require.NoError(t, err)
	require.Equal(t, "stale", current.Status)
}

func TestFindingsWaiverAndStale(t *testing.T) {
	t.Parallel()
	s, f := findingFixture(t)
	r, err := s.SaveFinding(t.Context(), "session", f, 0)
	require.NoError(t, err)
	f.Status = "waived"
	f.WaiverReason = "Model says user approved"
	_, err = s.SaveFinding(t.Context(), "session", f, r.Revision)
	require.Error(t, err, "model text does not authorize human waiver")
	require.True(t, s.FindingBlocks(t.Context(), f, f.SourceFingerprint))
	_, err = s.WaiveFinding(t.Context(), "session", f.ID, "Accepted compatibility limitation", func(context.Context, Finding, string) (bool, error) { return false, nil })
	require.Error(t, err)
	_, err = s.WaiveFinding(t.Context(), "session", f.ID, "Accepted compatibility limitation", func(context.Context, Finding, string) (bool, error) { return true, nil })
	require.NoError(t, err)
	waived, _, err := s.ReadFinding(t.Context(), "session", f.ID)
	require.NoError(t, err)
	require.Equal(t, "waived", waived.Status)
	require.NotEqual(t, "verified", waived.Status)
	require.False(t, s.FindingBlocks(t.Context(), waived, f.SourceFingerprint))
	require.True(t, s.FindingBlocks(t.Context(), waived, Hash("changed source")))
	f.Severity = 3
	require.False(t, s.FindingBlocks(t.Context(), f, f.SourceFingerprint))
}
