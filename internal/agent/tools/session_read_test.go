package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestSessionReadListsAndPages(t *testing.T) {
	t.Parallel()
	conn, err := db.Connect(t.Context(), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	q := db.New(conn)
	sessions := session.NewService(q, conn)
	messages := message.NewService(q)
	past, err := sessions.Create(t.Context(), "old migration")
	require.NoError(t, err)
	current, err := sessions.Create(t.Context(), "today")
	require.NoError(t, err)
	for i := range 15 {
		_, err := messages.Create(t.Context(), past.ID, message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("message number %d", i)}},
		})
		require.NoError(t, err)
	}
	tool := NewSessionReadTool(sessions, messages, func(context.Context) string { return current.ID })
	run := func(p SessionReadParams) fantasy.ToolResponse {
		in, err := json.Marshal(p)
		require.NoError(t, err)
		res, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "c", Name: SessionReadToolName, Input: string(in)})
		require.NoError(t, err)
		return res
	}

	list := run(SessionReadParams{})
	require.Contains(t, list.Content, past.ID)
	require.NotContains(t, list.Content, current.ID)

	page := run(SessionReadParams{SessionID: past.ID, Limit: 10})
	require.Contains(t, page.Content, "Messages 0-9 of 15")
	require.Contains(t, page.Content, "message number 9")
	require.Contains(t, page.Content, "next_offset: 10")

	last := run(SessionReadParams{SessionID: past.ID, Offset: 10})
	require.Contains(t, last.Content, "message number 14")
	require.NotContains(t, last.Content, "next_offset")

	require.True(t, run(SessionReadParams{SessionID: "nope"}).IsError)
}
