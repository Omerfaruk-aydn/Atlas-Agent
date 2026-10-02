package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestTodosQualityPersistsEvidenceAndRejectsInvalidUpdate(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { require.NoError(t, db.Release(dir)) })
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	sessions := session.NewService(db.New(conn), conn)
	created, err := sessions.Create(t.Context(), "Acceptance ledger")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, created.ID)
	tool := NewTodosTool(sessions)
	params := TodosParams{Todos: []TodoItem{{Content: "Build feature", Status: "completed", AcceptanceCriteria: []string{"Build passes"}, Verification: "passed", Evidence: []session.TodoEvidence{{Kind: "command", Detail: "go build: exit 0"}}}}}
	input, err := json.Marshal(params)
	require.NoError(t, err)
	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "valid", Name: TodosToolName, Input: string(input)})
	require.NoError(t, err)
	require.False(t, response.IsError)
	fetched, err := sessions.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, params.Todos[0].Evidence, fetched.Todos[0].Evidence)
	require.Equal(t, params.Todos[0].AcceptanceCriteria, fetched.Todos[0].AcceptanceCriteria)
	params.Todos[0].Verification = "failed"
	input, err = json.Marshal(params)
	require.NoError(t, err)
	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "invalid", Name: TodosToolName, Input: string(input)})
	require.NoError(t, err)
	require.True(t, response.IsError)
	unchanged, err := sessions.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, fetched.Todos, unchanged.Todos)
	params.Todos[0].AcceptanceCriteria = nil
	params.Todos[0].Evidence = nil
	params.Todos[0].Verification = ""
	input, err = json.Marshal(params)
	require.NoError(t, err)
	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "omitted", Name: TodosToolName, Input: string(input)})
	require.NoError(t, err)
	require.False(t, response.IsError)
	unchanged, err = sessions.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, fetched.Todos, unchanged.Todos, "omitting quality fields must preserve the existing acceptance ledger")
	params.Todos[0].AcceptanceCriteria = []string{"New requirement passes"}
	input, err = json.Marshal(params)
	require.NoError(t, err)
	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "stale", Name: TodosToolName, Input: string(input)})
	require.NoError(t, err)
	require.True(t, response.IsError, "changed criteria cannot inherit passing evidence")
	params.Todos[0].AcceptanceCriteria = nil
	params.Todos[0].Status = "in_progress"
	input, err = json.Marshal(params)
	require.NoError(t, err)
	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "reopen", Name: TodosToolName, Input: string(input)})
	require.NoError(t, err)
	require.False(t, response.IsError)
	reopened, err := sessions.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", reopened.Todos[0].Verification)
	require.Empty(t, reopened.Todos[0].Evidence)
	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "read", Name: TodosToolName, Input: `{"action":"list"}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Contains(t, response.Content, "Build passes")
}
