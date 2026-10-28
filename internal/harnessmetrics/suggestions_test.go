package harnessmetrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProposeImprovementsRequiresRecurringEvidence(t *testing.T) {
	t.Parallel()
	cases := []Case{
		{ID: "one", Request: "Fix tests", Reason: "contract unmet", Checks: []string{"go test ./..."}},
		{ID: "two", Request: "Fix other tests", Reason: "contract unmet", Checks: []string{"go test ./..."}},
		{ID: "single", Request: "Fix lint", Reason: "lint failed", Checks: []string{"task lint"}},
		{ID: "empty", Request: "Unrelated"},
	}
	proposals := ProposeImprovements(cases)
	require.Len(t, proposals, 1)
	proposal := proposals[0]
	require.Equal(t, "rule", proposal.Kind)
	require.Equal(t, []string{"one", "two"}, proposal.EvidenceCases)
	require.True(t, proposal.ABRequired)
	require.False(t, proposal.AutoApply)
}

func TestFailureSignatureNormalizesVolatileValues(t *testing.T) {
	t.Parallel()
	a := Case{Reason: "test timeout after 10s", Checks: []string{"go test"}}
	b := Case{Reason: "test timeout after 25s", Checks: []string{"go test"}}
	require.Equal(t, failureSignature(a), failureSignature(b))
	require.Len(t, ProposeImprovements([]Case{
		{ID: "a", Request: "one", Reason: a.Reason, Checks: a.Checks},
		{ID: "b", Request: "two", Reason: b.Reason, Checks: b.Checks},
	}), 1)
}
