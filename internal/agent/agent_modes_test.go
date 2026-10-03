package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

func TestPersistentSpecialistReusesSessionAndChargesOnlyDelta(t *testing.T) {
	env := testEnv(t)
	c := newTestCoordinator(t, env, "mock", config.ProviderConfig{ID: "mock"})
	c.engineering = engineering.NewStore(t.TempDir())
	parent, err := env.sessions.Create(t.Context(), "parent")
	require.NoError(t, err)
	var ids []string
	worker := newMockAgent("mock", 4096, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		ids = append(ids, call.SessionID)
		child, err := env.sessions.Get(ctx, call.SessionID)
		require.NoError(t, err)
		child.Cost += 2
		_, err = env.sessions.Save(ctx, child)
		require.NoError(t, err)
		return agentResultWithText("done"), nil
	})
	for i := range 2 {
		response, err := c.runSubAgent(t.Context(), subAgentParams{Agent: worker, SessionID: parent.ID, PersistentSessionID: "specialist-stable", AgentMessageID: fmt.Sprint(i), ToolCallID: fmt.Sprint(i), Prompt: "continue"})
		require.NoError(t, err)
		require.False(t, response.IsError)
	}
	require.Equal(t, []string{"specialist-stable", "specialist-stable"}, ids)
	parent, err = env.sessions.Get(t.Context(), parent.ID)
	require.NoError(t, err)
	require.Equal(t, float64(4), parent.Cost)
	children, err := env.sessions.ListByParent(t.Context(), parent.ID)
	require.NoError(t, err)
	require.Len(t, children, 1)
}

func TestAgentBatchRetriesOnlyFailedRowsAndRejectsChangedDefinition(t *testing.T) {
	env := testEnv(t)
	c := newTestCoordinator(t, env, "mock", config.ProviderConfig{ID: "mock"})
	c.engineering = engineering.NewStore(t.TempDir())
	parent, err := env.sessions.Create(t.Context(), "parent")
	require.NoError(t, err)
	counts := map[string]int{}
	worker := newMockAgent("mock", 4096, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		key := "good"
		if strings.HasPrefix(call.Prompt, "Review bad") {
			key = "bad"
		}
		counts[key]++
		if key == "bad" && counts[key] == 1 {
			return nil, fmt.Errorf("temporary failure")
		}
		return agentResultWithText("observed"), nil
	})
	cache := csync.NewMap[string, SessionAgent]()
	cache.Set("review", worker)
	p := agentToolCallParams{sessionID: parent.ID, agentMessageID: "message", toolCallID: "call", agentName: "review", discovered: []*subagents.Subagent{{Name: "review", ReadOnly: true}}, subagentInstances: cache, limiter: newConcurrencyLimiter(2)}
	args := AgentParams{Mode: "batch", AgentName: "review", BatchID: "audit", Prompt: "Review {{item}}", Items: []AgentBatchItem{{ID: "a", Input: "good"}, {ID: "b", Input: "bad"}}}
	response, err := c.runAgentMode(t.Context(), args, p)
	require.NoError(t, err)
	require.False(t, response.IsError)
	var report agentBatchReport
	require.NoError(t, json.Unmarshal([]byte(response.Content), &report))
	require.Equal(t, "succeeded", report.Rows[0].Status)
	require.Equal(t, "failed", report.Rows[1].Status)
	args.RetryFailed = true
	p.toolCallID = "retry"
	response, err = c.runAgentMode(t.Context(), args, p)
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Equal(t, map[string]int{"good": 1, "bad": 2}, counts)
	response, err = c.runAgentMode(t.Context(), AgentParams{Mode: "batch", BatchID: "audit", RetryFailed: true, Prompt: "stored retry"}, p)
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Equal(t, map[string]int{"good": 1, "bad": 2}, counts)
	args.Items[0].Input = "changed"
	response, err = c.runAgentMode(t.Context(), args, p)
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "definition changed")
}

func TestAgentBatchDuplicateIDsRejected(t *testing.T) {
	t.Parallel()
	require.Error(t, validateAgentBatch(AgentParams{BatchID: "id", Prompt: "{{item}}", Items: []AgentBatchItem{{ID: "same", Input: "a"}, {ID: "same", Input: "b"}}}))
}

func TestArchitectEditorRejectsOwnershipExpansion(t *testing.T) {
	t.Parallel()
	c := &coordinator{}
	ctx := engineering.WithOwnership(engineering.WithScope(t.Context(), "parent", "task"), t.TempDir(), []string{"src"})
	response, err := c.runArchitectEditor(ctx, AgentParams{OwnedPaths: []string{"."}}, agentToolCallParams{sessionID: "parent"})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "expand parent ownership")
}
