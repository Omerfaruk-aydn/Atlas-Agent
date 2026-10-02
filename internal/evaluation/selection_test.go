package evaluation

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func selectionFixture(t *testing.T) (SelectionPolicy, []LiveRecord, time.Time) {
	t.Helper()
	now := time.Unix(1800000000, 0)
	scenarios, err := Scenarios()
	require.NoError(t, err)
	policy := SelectionPolicy{Role: "backend", Scenarios: []string{scenarios[0].ID}, Candidates: []string{"p/cheap", "p/strong"}, PromptVersion: "v1"}
	var records []LiveRecord
	for _, model := range policy.Candidates {
		for i := range 3 {
			r := LiveRecord{Run: Run{Role: "backend", Model: model, Scenario: scenarios[0].ID, PromptVersion: "v1", Cost: 0.01, DurationMS: 100}, RecordedAt: now.Unix(), MetricsSource: "cumulative_runtime_ledger", Workspace: fmt.Sprintf("%s/%d", model, i), SessionID: fmt.Sprint(i)}
			for _, criterion := range scenarios[0].Criteria {
				r.Checks = append(r.Checks, Check{Criterion: criterion, Passed: true, Evidence: "checker exit 0"})
			}
			if model == "p/strong" {
				r.Cost = 0.02
			}
			r.Baseline = "base-commit"
			r.FixtureHash = "fixture-hash"
			records = append(records, r)
		}
	}
	return policy, records, now
}

func TestMeasuredSelectionRequiresComparableFreshEvidence(t *testing.T) {
	t.Parallel()
	p, r, now := selectionFixture(t)
	got, err := Recommend(p, r, now)
	require.NoError(t, err)
	require.Equal(t, "p/cheap", got.Model)
	r[0].ExecutionError = "provider failed"
	got, err = Recommend(p, r, now)
	require.NoError(t, err)
	require.Equal(t, "p/strong", got.Model)
	p.MaxMeanCost = 0.015
	_, err = Recommend(p, r, now)
	require.ErrorContains(t, err, "no candidate")
	p.MaxMeanCost = 0
	_, err = Recommend(p, r[:5], now)
	require.ErrorContains(t, err, "insufficient")
	r[0].MetricsSource = "session_context_fallback"
	_, err = Recommend(p, r, now)
	require.ErrorContains(t, err, "metrics")
	r[0].MetricsSource = "cumulative_runtime_ledger"
	r = append(r, r[0])
	_, err = Recommend(p, r, now)
	require.ErrorContains(t, err, "duplicate")
	_, err = Recommend(p, r[:6], now.Add(8*24*time.Hour))
	require.ErrorContains(t, err, "insufficient")
}

func TestComparisonSupportsRepeatedRunsAndSeparatesRoles(t *testing.T) {
	t.Parallel()
	_, records, _ := selectionFixture(t)
	scenarios, err := Scenarios()
	require.NoError(t, err)
	var runs []Run
	for _, record := range records {
		runs = append(runs, record.Run)
	}
	other := runs[0]
	other.Role = "frontend"
	runs = append(runs, other)
	groups, err := Compare(scenarios, runs)
	require.NoError(t, err)
	require.Len(t, groups, 3)
	require.Equal(t, 3, groups[0].Report.Evaluated)
	require.Equal(t, 3, groups[0].Report.Successful)
	require.Equal(t, "backend", groups[0].Role)
	require.Equal(t, "frontend", groups[2].Role)
	require.Equal(t, 1, groups[2].Report.Evaluated)
}
