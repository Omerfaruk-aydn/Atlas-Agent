package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/script"
	"github.com/stretchr/testify/require"
)

func testScriptHost(t *testing.T, mock SessionAgent) *scriptHost {
	t.Helper()
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})
	parent, err := env.sessions.Create(t.Context(), "Parent")
	require.NoError(t, err)
	cache := csync.NewMap[string, SessionAgent]()
	cache.Set("alpha", mock)
	return &scriptHost{
		c: coord, agentCfg: coord.cfg.Config().Agents[config.AgentTask],
		subagentInstances: cache, limiter: newConcurrencyLimiter(0),
		defaultAgent: func(context.Context) (SessionAgent, error) { return mock, nil },
		sessionID:    parent.ID, agentMessageID: "msg-1", callID: "call-1",
	}
}

func TestScriptRunsRealSubAgentsInParallelWithSchemas(t *testing.T) {
	var calls atomic.Int64
	mock := newMockAgent("test-provider", 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		calls.Add(1)
		return agentResultWithText(`{"ok": true, "n": 7}`), nil
	})
	host := testScriptHost(t, mock)

	res, err := script.Run(t.Context(), `
SCHEMA = {"type": "object", "required": ["ok"], "properties": {"ok": {"type": "boolean"}, "n": {"type": "integer"}}}

def main():
    rs = parallel([lambda: agent("check a", agent_name="alpha", schema=SCHEMA), lambda: agent("check b", agent_name="alpha", schema=SCHEMA)])
    return {"all_ok": all([r["ok"] for r in rs]), "sum": sum([r["n"] for r in rs])}
`, host, script.Limits{})
	require.NoError(t, err)
	require.JSONEq(t, `{"all_ok":true,"sum":14}`, res.Value)
	require.Equal(t, 2, res.Agents)
	require.Equal(t, int64(2), calls.Load())
}

func TestScriptUsesTheDefaultAgentWhenNoneIsNamed(t *testing.T) {
	mock := newMockAgent("test-provider", 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return agentResultWithText("default agent here"), nil
	})
	res, err := script.Run(t.Context(), "def main():\n    return agent(\"hello\")\n", testScriptHost(t, mock), script.Limits{})
	require.NoError(t, err)
	require.Contains(t, res.Value, "default agent here")
}

func TestScriptFailedAgentRunsSurfaceTheirReason(t *testing.T) {
	mock := newMockAgent("test-provider", 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return agentResultWithText("still not json"), nil
	})
	_, err := script.Run(t.Context(), `
def main():
    return agent("x", agent_name="alpha", schema={"type": "object"})
`, testScriptHost(t, mock), script.Limits{})
	require.ErrorContains(t, err, "did not match result_schema")
}

func TestScriptReportAndFailureTextsCarryTheLog(t *testing.T) {
	text := scriptReport(script.Result{Value: `{"a":1}`, Log: "== scan ==", Agents: 2})
	require.Contains(t, text, `{"a":1}`)
	require.Contains(t, text, "== scan ==")
	require.Contains(t, text, "2 agent(s) ran")

	failure := scriptFailure(script.ErrBudget, script.Result{Agents: 3})
	require.Contains(t, failure, "The script failed")
	require.Contains(t, failure, "max_agents")
	require.Contains(t, failure, "3 agent(s)")
}

func TestScriptToolIsNotHandedToSubAgents(t *testing.T) {
	require.Contains(t, subagentSpawningTools, ScriptToolName)
}
