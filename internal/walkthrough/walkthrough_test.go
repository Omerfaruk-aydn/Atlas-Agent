package walkthrough

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviations(t *testing.T) {
	t.Parallel()
	unplanned, untouched := Deviations([]string{"a.go", "./b.go", "c/d.go"}, []string{"a.go", "b.go", "extra.go"})
	require.Equal(t, []string{"extra.go"}, unplanned)
	require.Equal(t, []string{"c/d.go"}, untouched)
}

func TestRender(t *testing.T) {
	t.Parallel()
	out := Render(Input{
		Goal:         "Add the thing",
		PlanDigest:   "abc123",
		PlannedPaths: []string{"a.go", "b.go"},
		ChangedPaths: []string{"b.go", "z.go"},
		Checks:       []Check{{"go test ./...", true}, {"go vet ./...", false}},
		RedGreen:     []RedGreen{{"go test ./pkg"}},
		OpenIssues:   []string{"flaky timeout"},
	})
	require.Contains(t, out, "**Goal:** Add the thing")
	require.Contains(t, out, "plan abc123")
	require.Contains(t, out, "Changed but not in the plan:\n- `z.go`")
	require.Contains(t, out, "In the plan but not changed:\n- `a.go`")
	require.Contains(t, out, "- pass `go test ./...`")
	require.Contains(t, out, "- FAIL `go vet ./...`")
	require.Contains(t, out, "Failed before the change and passed after it")
	require.Contains(t, out, "flaky timeout")
}

func TestRenderWithoutEvidenceSaysSo(t *testing.T) {
	t.Parallel()
	out := Render(Input{})
	require.Contains(t, out, "No files changed.")
	require.Contains(t, out, "Nothing here was verified by the harness.")
	require.NotContains(t, out, "Plan vs.")
}
