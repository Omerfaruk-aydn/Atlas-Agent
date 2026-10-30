package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchitectPlanEnforcesStructuredBoundedScope(t *testing.T) {
	t.Parallel()
	valid := `{"changes":[{"file":"internal/a.go","intent":"handle empty input"}],"invariants":["public API unchanged"],"verification":["go test ./internal"]}`
	plan, err := parseArchitectPlan(valid, []string{"internal"})
	require.NoError(t, err)
	require.Len(t, plan.Changes, 1)
	for _, invalid := range []string{
		`write internal/a.go`,
		`{"changes":[],"verification":["test"]}`,
		`{"changes":[{"file":"../escape.go","intent":"escape"}],"verification":["test"]}`,
		`{"changes":[{"file":"other/a.go","intent":"outside"}],"verification":["test"]}`,
		valid + ` {}`,
	} {
		_, err := parseArchitectPlan(invalid, []string{"internal"})
		require.Error(t, err, invalid)
	}
}
