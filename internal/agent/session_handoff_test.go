package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestHandoffCreatesLinkedSessionWithDurablePacket(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	worker := testSessionAgent(env, &finishStreamModel{text: "unused"}, &finishStreamModel{text: "unused"}, "system").(*sessionAgent)
	source, err := env.sessions.Create(t.Context(), "source")
	require.NoError(t, err)
	require.NoError(t, env.sessions.SetGoal(t.Context(), source.ID, "Ship the exact goal"))
	impl := worker
	ledger := impl.evaluatorLedgerFor(source.ID)
	ledger.files = []string{"internal/example.go"}
	ledger.runs = map[string]evaluatorCommand{}
	ledger.runs["go test ./internal/example"] = evaluatorCommand{command: "go test ./internal/example", failed: false}

	child, err := impl.Handoff(t.Context(), source.ID)
	require.NoError(t, err)
	require.Equal(t, source.ID, child.ParentSessionID)
	require.NotEmpty(t, child.SummaryMessageID)
	summary, err := env.messages.Get(t.Context(), child.SummaryMessageID)
	require.NoError(t, err)
	text := summary.Content().Text
	require.Contains(t, text, "Ship the exact goal")
	require.Contains(t, text, "internal/example.go")
	require.Contains(t, text, "PASS: go test ./internal/example")
	require.Contains(t, text, "do not treat old evidence as current proof")
	require.Contains(t, text, source.ID)
}

func TestSessionHandoffTextPreservesVerbatimConstraints(t *testing.T) {
	t.Parallel()
	source := session.Session{ID: "parent", Goal: "goal"}
	text := sessionHandoffText(source, nil, "Decision: keep API stable", []engineering.UserTurn{{Kind: "user", Text: "Do not commit\nRun all tests"}}, nil, nil)
	require.Contains(t, text, "Do not commit\n  Run all tests")
	require.Contains(t, text, "Decision: keep API stable")
}
