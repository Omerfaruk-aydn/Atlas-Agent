package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/issues"
	"github.com/stretchr/testify/require"
)

func TestOpenIssuesSendTheTurnBack(t *testing.T) {
	t.Parallel()
	const session = "open-issues-gate-session"
	a := &sessionAgent{}

	decision, stop := a.deterministicChecks(t.Context(), session, &evaluatorLedger{})
	require.False(t, stop, "no issues recorded: the gate passes")
	require.Empty(t, decision.Continue)

	id, err := tools.IssuesFor(session).Add("race in cache", issues.Major, "cache.go")
	require.NoError(t, err)
	decision, stop = a.deterministicChecks(t.Context(), session, &evaluatorLedger{})
	require.True(t, stop)
	require.Contains(t, decision.Continue, "race in cache")

	require.NoError(t, tools.IssuesFor(session).Close(id, "go test -race ./cache", "ok  \tcache\t1.1s"))
	_, stop = a.deterministicChecks(t.Context(), session, &evaluatorLedger{})
	require.False(t, stop, "closed with evidence: the gate passes again")
}
