package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func runIssues(t *testing.T, session string, p IssuesParams) fantasy.ToolResponse {
	t.Helper()
	in, err := json.Marshal(p)
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, session)
	res, err := NewIssuesTool().Run(ctx, fantasy.ToolCall{ID: "c", Name: IssuesToolName, Input: string(in)})
	require.NoError(t, err)
	return res
}

func TestIssuesToolBlocksCompletionUntilClosedWithEvidence(t *testing.T) {
	t.Parallel()
	session := "issues-test-" + t.Name()
	require.Empty(t, OpenIssuesBlockingCompletion(session))

	require.False(t, runIssues(t, session, IssuesParams{Action: "add", Title: "race in cache", Severity: "major", Where: "cache.go"}).IsError)
	require.False(t, runIssues(t, session, IssuesParams{Action: "add", Title: "typo", Severity: "minor"}).IsError)
	require.Contains(t, OpenIssuesBlockingCompletion(session), "race in cache")
	require.NotContains(t, OpenIssuesBlockingCompletion(session), "typo")

	require.True(t, runIssues(t, session, IssuesParams{Action: "close", ID: 1}).IsError)
	require.True(t, runIssues(t, session, IssuesParams{Action: "add", Title: "x", Severity: "bad"}).IsError)
	require.False(t, runIssues(t, session, IssuesParams{Action: "close", ID: 1, Command: "go test -race ./cache", Output: "ok  \tcache\t1.2s"}).IsError)
	require.Empty(t, OpenIssuesBlockingCompletion(session))
	require.Contains(t, runIssues(t, session, IssuesParams{Action: "list"}).Content, "closed by `go test -race ./cache`")
	require.True(t, runIssues(t, session, IssuesParams{Action: "frobnicate"}).IsError)
}
