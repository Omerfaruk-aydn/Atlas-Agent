package evaluation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScoreRequiresEvidenceAndDetectsFalseCompletion(t *testing.T) {
	t.Parallel()
	scenarios := []Scenario{{ID: "feature", Criteria: []string{"wired", "tested"}}}
	result, err := Score(scenarios, []Run{{Scenario: "feature", ClaimedComplete: true, Checks: []Check{{Criterion: "wired", Passed: true, Evidence: "Inspected caller"}, {Criterion: "tested", Passed: true}}, ToolCalls: 10, RepeatedToolCalls: 2, Tokens: 100, DurationMS: 3000}})
	require.NoError(t, err)
	require.Equal(t, 1, result.FalseCompletions)
	require.Equal(t, 0, result.Successful)
	require.Equal(t, 0.5, result.AcceptanceRate)
	require.Equal(t, int64(100), result.Tokens)
	require.Equal(t, 0.2, result.RepeatedCallRate)
}

func TestScoreRejectsUnknownDuplicateAndInvalidMetrics(t *testing.T) {
	t.Parallel()
	scenarios := []Scenario{{ID: "feature", Criteria: []string{"wired"}}}
	for _, runs := range [][]Run{
		{{Scenario: "unknown"}},
		{{Scenario: "feature"}, {Scenario: "feature"}},
		{{Scenario: "feature", Tokens: -1}},
		{{Scenario: "feature", ToolCalls: 1, RepeatedToolCalls: 2}},
		{{Scenario: "feature", Checks: []Check{{Criterion: "invented"}}}},
		{{Scenario: "feature", Checks: []Check{{Criterion: "wired"}, {Criterion: "wired"}}}},
	} {
		_, err := Score(scenarios, runs)
		require.Error(t, err)
	}
}

func TestEmbeddedScenariosCoverEngineeringAndUI(t *testing.T) {
	t.Parallel()
	scenarios, err := Scenarios()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(scenarios), 8)
	seen := map[string]bool{}
	for _, scenario := range scenarios {
		require.False(t, seen[scenario.ID])
		seen[scenario.ID] = true
		require.NotEmpty(t, scenario.Prompt)
		require.NotEmpty(t, scenario.Criteria)
	}
	require.True(t, seen["ui-states"])
	require.True(t, seen["resume-steering"])
}
