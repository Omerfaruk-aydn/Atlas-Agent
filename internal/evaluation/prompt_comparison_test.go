package evaluation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptComparisonRejectsConfoundedAndUnequalRuns(t *testing.T) {
	t.Parallel()
	scenarios := []Scenario{{ID: "fixture", Criteria: []string{"correct"}}}
	old := LiveRecord{Run: Run{Model: "provider/model", PromptVersion: "before", Scenario: "fixture", Tokens: 200, DurationMS: 100, Cost: 0.2, ToolCalls: 2, Checks: []Check{{Criterion: "correct", Passed: false, Evidence: "exit 1"}}}, CaseHash: "case", Baseline: "commit", SessionID: "old", MetricsSource: "cumulative_runtime_ledger", ClaimObserved: true}
	fresh := old
	fresh.PromptVersion, fresh.SessionID, fresh.Tokens = "after", "new", 100
	fresh.Checks = []Check{{Criterion: "correct", Passed: true, Evidence: "exit 0"}}
	r, err := ComparePromptVersions(scenarios, []LiveRecord{old}, []LiveRecord{fresh}, 1)
	require.NoError(t, err)
	require.Len(t, r, 1)
	require.Equal(t, float64(1), r[0].AcceptanceDelta)
	require.Equal(t, float64(-100), r[0].MeanTokensDelta)
	require.Nil(t, r[0].MeanUnnecessaryQuestionsDelta)
	old.QuestionReview = &QuestionReview{Total: 2, Unnecessary: 1, Evidence: "Reviewed transcript before"}
	fresh.QuestionReview = &QuestionReview{Total: 1, Unnecessary: 0, Evidence: "Reviewed transcript after"}
	r, err = ComparePromptVersions(scenarios, []LiveRecord{old}, []LiveRecord{fresh}, 1)
	require.NoError(t, err)
	require.Equal(t, float64(-1), *r[0].MeanUnnecessaryQuestionsDelta)
	_, err = ComparePromptVersions(scenarios, []LiveRecord{old}, []LiveRecord{fresh}, 3)
	require.Error(t, err)
	for _, mutate := range []func(*LiveRecord){
		func(r *LiveRecord) { r.CaseHash = "changed" },
		func(r *LiveRecord) { r.Baseline = "other commit" },
		func(r *LiveRecord) { r.Model = "different model" },
		func(r *LiveRecord) { r.Role = "different role" },
		func(r *LiveRecord) { r.MetricsSource = "estimated" },
		func(r *LiveRecord) { r.PromptVersion = "before" },
		func(r *LiveRecord) { r.SessionID = "old" },
	} {
		changed := fresh
		mutate(&changed)
		_, err = ComparePromptVersions(scenarios, []LiveRecord{old}, []LiveRecord{changed}, 1)
		require.Error(t, err)
	}
}
