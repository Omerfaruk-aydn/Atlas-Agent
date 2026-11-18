package agent

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/teams"
	"github.com/stretchr/testify/require"
)

func TestBoardNameFallsBackToTheSessionTitle(t *testing.T) {
	require.Equal(t, "reviewer", boardName(subAgentParams{BoardName: "reviewer", SessionTitle: "x"}))
	require.Equal(t, "alpha", boardName(subAgentParams{SessionTitle: "alpha agent session"}))
	require.Equal(t, "agent", boardName(subAgentParams{SessionTitle: "New Agent Session"}))
	require.Equal(t, "agent", boardName(subAgentParams{}))
}

// A sub-agent takes a handle on the team board for the length of its run and
// is marked done afterwards, so the stall monitor never reports a finished one.
func TestRunSubAgentJoinsTheBoardAndIsMarkedDone(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})
	coord.teams = teams.NewRegistry() // The test coordinator is built without one.
	parent, err := env.sessions.Create(t.Context(), "Parent")
	require.NoError(t, err)

	var during []teams.Member
	mock := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		during = coord.teams.Members(coord.teams.TeamFor(parent.ID))
		return agentResultWithText("ok"), nil
	})
	_, err = coord.runSubAgent(t.Context(), subAgentParams{
		Agent: mock, SessionID: parent.ID, AgentMessageID: "m1", ToolCallID: "c1",
		Prompt: "go", SessionTitle: "t", BoardName: "Code Reviewer",
	})
	require.NoError(t, err)

	names := map[string]teams.State{}
	for _, m := range during {
		names[m.Name] = m.State
	}
	require.Equal(t, teams.StateWorking, names["code-reviewer"], "the sub-agent is on the board while it runs")
	require.Contains(t, names, "lead", "the parent session is the lead")

	after := map[string]teams.State{}
	for _, m := range coord.teams.Members(coord.teams.TeamFor(parent.ID)) {
		after[m.Name] = m.State
	}
	require.Equal(t, teams.StateDone, after["code-reviewer"])
	require.Equal(t, teams.StateWorking, after["lead"])
}
