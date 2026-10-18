package evaluation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func rec(model, scenario string, attempt int, pass bool) LiveRecord {
	r := LiveRecord{Run: Run{Scenario: scenario, Model: model, Checks: []Check{{Criterion: "c", Passed: pass, Evidence: "exit status observed"}}}, Attempt: attempt, CaseHash: "h-" + scenario}
	return r
}

func TestSummarizeAttemptsSeparatesGenerationFromSelection(t *testing.T) {
	t.Parallel()

	// Attempt 0 fails, attempt 1 passes, but Atlas only observed its own
	// verification on attempt 0: the model could solve it, the selection
	// would deliver the wrong result.
	a0 := rec("m", "bug", 0, false)
	a0.AtlasVerified, a0.ClaimedComplete = true, true
	a1 := rec("m", "bug", 1, true)
	// A second scenario: first attempt passes and is verified.
	b0 := rec("m", "ui", 0, true)
	b0.AtlasVerified = true
	b1 := rec("m", "ui", 1, false)

	report, err := SummarizeAttempts([]LiveRecord{a1, a0, b0, b1})
	require.NoError(t, err)
	require.Len(t, report.Groups, 2)
	bug := report.Groups[0]
	require.Equal(t, "bug", bug.Scenario)
	require.False(t, bug.FirstPassed)
	require.True(t, bug.AnyPassed)
	require.Equal(t, 0, bug.SelectedAttempt)
	require.False(t, bug.SelectedPassed)
	require.True(t, bug.SelectionLoss)
	require.InDelta(t, 0.5, report.PassAt1, 1e-9)
	require.InDelta(t, 1.0, report.PassAtK, 1e-9)
	require.InDelta(t, 0.5, report.SelectedRate, 1e-9)
	require.Equal(t, 1, report.SelectionLosses)
	require.Equal(t, 1, report.FalseCompletions)
}

func TestProviderFailuresDoNotCountAgainstTheModel(t *testing.T) {
	t.Parallel()

	r := rec("m", "bug", 0, false)
	r.ExecutionError = "exit status 1: 429 Too Many Requests"
	r.ErrorKind = ClassifyError(r.ExecutionError, "")
	require.Equal(t, ErrorProvider, r.ErrorKind)
	report, err := SummarizeAttempts([]LiveRecord{r})
	require.NoError(t, err)
	require.Equal(t, 1, report.ProviderErrors)
	require.Zero(t, report.PassAt1, "excluded, not failed")
	require.Equal(t, 0, report.Groups[0].Valid)

	require.Equal(t, ErrorTimeout, ClassifyError("context deadline exceeded", ""))
	require.Equal(t, ErrorAgent, ClassifyError("", "Tool loop: invalid arguments"))
	require.Equal(t, ErrorProvider, ClassifyError("402 Payment Required: insufficient_balance_error", ""))
	require.Equal(t, ErrorProvider, ClassifyError("Function not found for account", ""))
	require.Empty(t, ClassifyError("", ""))
}

func TestCriteriaCannotChangeBetweenAttempts(t *testing.T) {
	t.Parallel()

	a := rec("m", "bug", 0, true)
	b := rec("m", "bug", 1, true)
	b.CaseHash = "changed"
	_, err := SummarizeAttempts([]LiveRecord{a, b})
	require.ErrorContains(t, err, "criteria cannot change")
}

func TestAttemptNumbersAndProviderFailures(t *testing.T) {
	t.Parallel()
	a := rec("m", "bug", 2, false)
	a.ErrorKind = ErrorProvider
	b := rec("m", "bug", 5, true)
	b.AtlasVerified = true
	r, err := SummarizeAttempts([]LiveRecord{a, b})
	require.NoError(t, err)
	require.Equal(t, 5, r.Groups[0].SelectedAttempt)
	require.Equal(t, 1.0, r.PassAt1)
	require.Contains(t, r.EvidenceBasis, "not an observed")
	_, err = SummarizeAttempts([]LiveRecord{b, b})
	require.ErrorContains(t, err, "duplicate attempt")
}

func TestProviderFailureCannotBecomeTaskSuccess(t *testing.T) {
	t.Parallel()
	a := rec("m", "bug", 0, true)
	a.ErrorKind = ErrorProvider
	a.AtlasVerified = true
	b := rec("m", "bug", 1, true)
	r, err := SummarizeAttempts([]LiveRecord{a, b})
	require.NoError(t, err)
	require.Equal(t, 1, r.Groups[0].SelectedAttempt)
	require.Equal(t, 1, r.Groups[0].Valid)
	require.Equal(t, 1.0, r.SelectedRate)
}
