package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVictoryCandidatesAreRecentPassingVerifications(t *testing.T) {
	t.Parallel()
	l := &evaluatorLedger{lastEdit: 5, runs: map[string]evaluatorCommand{
		"a": {command: "go test ./...", seq: 9},
		"b": {command: "go build ./...", seq: 8},
		"c": {command: "go vet ./...", seq: 7},
		"d": {command: "npm run lint", seq: 6},
		"e": {command: "go test ./old", seq: 3},
		"f": {command: "go test ./bad", seq: 10, failed: true},
		"g": {command: "ls -la", seq: 11},
	}}
	got := victoryCandidates(l)
	require.Len(t, got, 3)
	require.Equal(t, "go test ./...", got[0].command)
	require.Equal(t, "go vet ./...", got[2].command)
}

func TestVictoryAuditIsOffByDefault(t *testing.T) {
	t.Parallel()
	a := &sessionAgent{}
	decision, stop := a.victoryAudit(t.Context(), "s", &evaluatorLedger{runs: map[string]evaluatorCommand{"a": {command: "go test ./...", seq: 2}}})
	require.False(t, stop)
	require.Empty(t, decision.Continue)
}
