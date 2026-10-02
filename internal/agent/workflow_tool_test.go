package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

func TestWorkflowDispatchesReadyWaveAndRequiresIndependentVerification(t *testing.T) {
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "workflow")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{ID: "api", Content: "API", Status: session.TodoStatusPending, Agent: "backend", OwnedPaths: []string{"api"}, AcceptanceCriteria: []string{"API tested"}}, {ID: "docs", Content: "Docs", Status: session.TodoStatusPending, Agent: "docs", OwnedPaths: []string{"docs"}, AcceptanceCriteria: []string{"Docs inspected"}}, {ID: "integrate", Content: "Integrate", Status: session.TodoStatusPending, Agent: "backend", DependsOn: []string{"api", "docs"}, OwnedPaths: []string{"."}, AcceptanceCriteria: []string{"Integration passes"}}}
	_, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{MaxConcurrentSubAgents: 2}}).Scoped(t.TempDir()), sessions: env.sessions, permissions: env.permissions, engineering: engineering.NewStore(t.TempDir())}
	var mu sync.Mutex
	var tasks []string
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		scope := engineering.GetScope(ctx, "")
		var input AgentParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
		require.Contains(t, input.Prompt, "<task_context>")
		require.Contains(t, input.Prompt, `"task_id":"`+scope.TaskID+`"`)
		for _, task := range sess.Todos {
			if task.ID == scope.TaskID {
				require.Contains(t, input.Prompt, session.TaskFingerprint(task))
			}
		}
		mu.Lock()
		tasks = append(tasks, scope.TaskID)
		mu.Unlock()
		return fantasy.NewTextResponse(workflowJSON(subagents.Handoff{TaskID: scope.TaskID, Summary: "Assignment implemented; coordinator must verify", ChangedFiles: []string{}, Checks: []subagents.HandoffCheck{}, Risks: []string{}, Dependencies: []string{}, Decision: "ready"})), nil
	}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	resp, err := c.workflowTool(invoke).Run(ctx, fantasy.ToolCall{ID: "wave", Name: "workflow", Input: `{"action":"dispatch"}`})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.ElementsMatch(t, []string{"api", "docs"}, tasks)
	stored, err := env.sessions.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, session.TodoStatusInProgress, stored.Todos[0].Status)
	require.Equal(t, session.TodoStatusPending, stored.Todos[2].Status)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &result))
	require.NotEmpty(t, result["next"])
	state, err := c.engineering.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, state.RoleExecutions["api"].Handoff)
	require.True(t, state.RoleExecutions["api"].RequireReview)
	require.False(t, state.RoleExecutions["api"].Passed)
	wave, err := session.ReadyTaskWave(stored.Todos, 4)
	require.NoError(t, err)
	require.Empty(t, wave)
}
