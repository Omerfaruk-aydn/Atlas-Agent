package agent

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestWorkersGateLetsAnOrchestratorFinishWithNothingPending(t *testing.T) {
	t.Parallel()
	l := &evaluatorLedger{}
	_, stop := workersDecision(workerState{Collected: 3}, l)
	require.False(t, stop)
}

func TestWorkersGateSendsBackWhileWorkersRunOrAreUnread(t *testing.T) {
	t.Parallel()
	l := &evaluatorLedger{}
	decision, stop := workersDecision(workerState{Running: []string{"agent-1 (backend: api)"}, Unread: []string{"agent-2"}}, l)
	require.True(t, stop)
	require.Contains(t, decision.Continue, "still running:\n- agent-1 (backend: api)")
	require.Contains(t, decision.Continue, "answers were not read:\n- agent-2")
	require.Contains(t, decision.Continue, "agent_result with wait=true and no id")
	require.Empty(t, decision.Unresolved)
}

func TestWorkersGateWaitsAsLongAsTheOrchestratorMakesProgress(t *testing.T) {
	t.Parallel()
	l := &evaluatorLedger{}
	pending := workerState{Running: []string{"agent-1"}}
	for range maxIdleWorkerRounds {
		decision, stop := workersDecision(pending, l)
		require.True(t, stop)
		require.NotEmpty(t, decision.Continue)
	}
	// Collecting an answer restarts the count.
	pending.Collected = 1
	decision, stop := workersDecision(pending, l)
	require.True(t, stop)
	require.NotEmpty(t, decision.Continue, "progress earns another round")

	for range maxIdleWorkerRounds - 1 {
		_, _ = workersDecision(pending, l)
	}
	decision, stop = workersDecision(pending, l)
	require.True(t, stop)
	require.Empty(t, decision.Continue)
	require.Contains(t, decision.Unresolved, "without collecting them")
}

func TestWorkersGateOnlyAppliesToTheOrchestratorMode(t *testing.T) {
	c := newSessionModeCoordinator(t, "")
	a := &sessionAgent{taskContextConfig: c.cfg}
	require.False(t, a.orchestrating())

	c.cfg.Config().Options.SessionMode = " Orchestrator "
	require.True(t, a.orchestrating())
	a.isSubAgent = true
	require.False(t, a.orchestrating(), "a worker is never held back by its own gate")
	a.isSubAgent = false

	// The gate reads the shared registry by session.
	session := "orchestrator-gate-session"
	hold := make(chan struct{})
	_, err := sharedBackgroundRuns.start(t.Context(), session, func(context.Context) (fantasy.ToolResponse, error) {
		<-hold
		return fantasy.NewTextResponse("done"), nil
	})
	require.NoError(t, err)
	decision, stop := a.workersGate(session, &evaluatorLedger{})
	require.True(t, stop)
	require.NotEmpty(t, decision.Continue)

	c.cfg.Config().Options.SessionMode = "review"
	_, stop = a.workersGate(session, &evaluatorLedger{})
	require.False(t, stop)
	close(hold)
}
