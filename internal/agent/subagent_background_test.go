package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func readResult(t *testing.T, tool fantasy.AgentTool, session, input string) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, session)
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "1", Name: AgentResultToolName, Input: input})
	require.NoError(t, err)
	return resp
}

func TestBackgroundRunsOutliveTheTurnAndAreReadBySession(t *testing.T) {
	t.Parallel()
	runs := newBackgroundRuns()
	tool := newAgentResultTool(runs)
	release := make(chan struct{})

	turn, endTurn := context.WithCancel(t.Context())
	id, err := runs.start(turn, "s1", func(ctx context.Context) (fantasy.ToolResponse, error) {
		select {
		case <-release:
			return fantasy.NewTextResponse("the answer"), nil
		case <-ctx.Done():
			return fantasy.ToolResponse{}, ctx.Err()
		}
	})
	require.NoError(t, err)
	endTurn() // The turn ends; the run must not.

	require.Contains(t, readResult(t, tool, "s1", `{"id":"`+id+`"}`).Content, "still running")
	require.True(t, readResult(t, tool, "s2", `{"id":"`+id+`"}`).IsError, "another session cannot read it")

	close(release)
	got := readResult(t, tool, "s1", `{"id":"`+id+`","wait":true,"wait_timeout":5}`)
	require.False(t, got.IsError)
	require.Equal(t, "the answer", got.Content)
}

func TestBackgroundRunCanBeStoppedAndFailuresAreReported(t *testing.T) {
	t.Parallel()
	runs := newBackgroundRuns()
	tool := newAgentResultTool(runs)

	stuck, err := runs.start(t.Context(), "s1", func(ctx context.Context) (fantasy.ToolResponse, error) {
		<-ctx.Done()
		return fantasy.ToolResponse{}, ctx.Err()
	})
	require.NoError(t, err)
	require.Contains(t, readResult(t, tool, "s1", `{"id":"`+stuck+`","cancel":true}`).Content, "Stopped")
	require.True(t, readResult(t, tool, "s1", `{"id":"`+stuck+`"}`).IsError)

	failed, err := runs.start(t.Context(), "s1", func(context.Context) (fantasy.ToolResponse, error) {
		return fantasy.ToolResponse{}, errors.New("model unavailable")
	})
	require.NoError(t, err)
	got := readResult(t, tool, "s1", `{"id":"`+failed+`","wait":true,"wait_timeout":5}`)
	require.True(t, got.IsError)
	require.Contains(t, got.Content, "model unavailable")

	require.True(t, readResult(t, tool, "s1", `{"cancel":true}`).IsError, "stopping needs an id")
}

func TestAgentResultWaitsForWhicheverWorkerFinishesNext(t *testing.T) {
	t.Parallel()
	runs := newBackgroundRuns()
	tool := newAgentResultTool(runs)
	first, second := make(chan struct{}), make(chan struct{})
	slow, err := runs.startNamed(t.Context(), "s1", "backend: build the API", func(context.Context) (fantasy.ToolResponse, error) {
		<-first
		return fantasy.NewTextResponse("api done"), nil
	})
	require.NoError(t, err)
	fast, err := runs.startNamed(t.Context(), "s1", "frontend: build the page", func(context.Context) (fantasy.ToolResponse, error) {
		<-second
		return fantasy.NewTextResponse("page done"), nil
	})
	require.NoError(t, err)

	list := readResult(t, tool, "s1", `{}`).Content
	require.Contains(t, list, slow+" (backend: build the API): running")
	require.Contains(t, list, fast+" (frontend: build the page): running")
	require.Contains(t, list, "2 running, 0 finished and unread")
	require.Equal(t, "No background sub-agents in this session.", readResult(t, tool, "s2", `{}`).Content)

	got := make(chan fantasy.ToolResponse, 1)
	go func() { got <- readResult(t, tool, "s1", `{"wait":true,"wait_timeout":10}`) }()
	select {
	case <-got:
		t.Fatal("waiting for the next worker returned before any finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(second)
	resp := <-got
	require.Contains(t, resp.Content, fast+" (frontend: build the page) finished.")
	require.Contains(t, resp.Content, "page done")

	require.Equal(t, workerState{Running: []string{slow + " (backend: build the API)"}, Collected: 1}, runs.pending("s1"))
	close(first)
	resp = readResult(t, tool, "s1", `{"wait":true,"wait_timeout":10}`)
	require.Contains(t, resp.Content, "api done")
	require.Equal(t, workerState{Collected: 2}, runs.pending("s1"))
	require.Contains(t, readResult(t, tool, "s1", `{"wait":true,"wait_timeout":1}`).Content, "No background sub-agent is running")
}

func TestAgentResultWaitForNextTimesOutWithAListing(t *testing.T) {
	t.Parallel()
	runs := newBackgroundRuns()
	tool := newAgentResultTool(runs)
	hold := make(chan struct{})
	defer close(hold)
	_, err := runs.start(t.Context(), "s1", func(context.Context) (fantasy.ToolResponse, error) { <-hold; return fantasy.ToolResponse{}, nil })
	require.NoError(t, err)
	resp := readResult(t, tool, "s1", `{"wait":true,"wait_timeout":1}`)
	require.Contains(t, resp.Content, "No sub-agent finished within 1s; 1 still running")
}

func TestUnreadAnswersCountAsPendingUntilRead(t *testing.T) {
	t.Parallel()
	runs := newBackgroundRuns()
	tool := newAgentResultTool(runs)
	id, err := runs.start(t.Context(), "s1", func(context.Context) (fantasy.ToolResponse, error) { return fantasy.NewTextResponse("x"), nil })
	require.NoError(t, err)
	run, _ := runs.get(id, "s1")
	<-run.done
	require.Equal(t, workerState{Unread: []string{id}}, runs.pending("s1"))
	require.Equal(t, "x", readResult(t, tool, "s1", `{"id":"`+id+`"}`).Content)
	require.Equal(t, workerState{Collected: 1}, runs.pending("s1"))
}

func TestBackgroundRunsAreCappedPerSession(t *testing.T) {
	t.Parallel()
	runs := newBackgroundRuns()
	hold := make(chan struct{})
	defer close(hold)
	block := func(context.Context) (fantasy.ToolResponse, error) { <-hold; return fantasy.ToolResponse{}, nil }
	for range maxBackgroundRuns {
		_, err := runs.start(t.Context(), "s1", block)
		require.NoError(t, err)
	}
	_, err := runs.start(t.Context(), "s1", block)
	require.Error(t, err)
	_, err = runs.start(t.Context(), "s2", block)
	require.NoError(t, err, "the cap is per session")
}

func TestFinishedBackgroundRunsAreForgottenAfterAWhile(t *testing.T) {
	t.Parallel()
	runs := newBackgroundRuns()
	clock := time.Now()
	runs.now = func() time.Time { return clock }
	id, err := runs.start(t.Context(), "s1", func(context.Context) (fantasy.ToolResponse, error) { return fantasy.NewTextResponse("x"), nil })
	require.NoError(t, err)
	run, _ := runs.get(id, "s1")
	<-run.done
	clock = clock.Add(backgroundKeep + time.Minute)
	_, err = runs.start(t.Context(), "s1", func(context.Context) (fantasy.ToolResponse, error) { return fantasy.ToolResponse{}, nil })
	require.NoError(t, err)
	_, ok := runs.get(id, "s1")
	require.False(t, ok)
}
