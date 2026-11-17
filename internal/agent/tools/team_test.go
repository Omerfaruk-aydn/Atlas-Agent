package tools

import (
	"context"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/teams"
	"github.com/stretchr/testify/require"
)

// fakeTeamRegistry is a minimal, in-test double for teamRegistry that
// records what it was asked to do without any of the real registry's
// blocking/timing behavior.
type fakeTeamRegistry struct {
	teamForCalls []string
	teamID       string

	sent []struct{ teamID, from, text string }

	waitMsgs    []teams.Message
	waitTimeout time.Duration
	waitSince   int
}

func (f *fakeTeamRegistry) TeamFor(sessionID string) string {
	f.teamForCalls = append(f.teamForCalls, sessionID)
	if f.teamID != "" {
		return f.teamID
	}
	return sessionID
}

func (f *fakeTeamRegistry) Send(teamID, from, text string) teams.Message {
	f.sent = append(f.sent, struct{ teamID, from, text string }{teamID, from, text})
	return teams.Message{Seq: len(f.sent), From: from, Text: text, Time: time.Now()}
}

func (f *fakeTeamRegistry) Wait(_ context.Context, _ string, since int, timeout time.Duration) []teams.Message {
	f.waitSince = since
	f.waitTimeout = timeout
	return f.waitMsgs
}

func runTeamSendTool(t *testing.T, reg *fakeTeamRegistry, sessionID, input string) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, sessionID)
	resp, err := NewTeamSendTool(reg).Run(ctx, fantasy.ToolCall{ID: "call", Name: TeamSendToolName, Input: input})
	require.NoError(t, err)
	return resp
}

func runTeamReadTool(t *testing.T, reg *fakeTeamRegistry, sessionID, input string) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, sessionID)
	resp, err := NewTeamReadTool(reg).Run(ctx, fantasy.ToolCall{ID: "call", Name: TeamReadToolName, Input: input})
	require.NoError(t, err)
	return resp
}

func TestTeamSendRequiresNonEmptyText(t *testing.T) {
	reg := &fakeTeamRegistry{}
	resp := runTeamSendTool(t, reg, "session-1", `{"text":"   "}`)
	require.True(t, resp.IsError)
	require.Empty(t, reg.sent)
}

func TestTeamSendRequiresASession(t *testing.T) {
	reg := &fakeTeamRegistry{}
	_, err := NewTeamSendTool(reg).Run(t.Context(), fantasy.ToolCall{ID: "call", Name: TeamSendToolName, Input: `{"text":"hi"}`})
	require.Error(t, err)
}

func TestTeamSendBroadcastsUnderTheCallersSessionID(t *testing.T) {
	reg := &fakeTeamRegistry{}
	resp := runTeamSendTool(t, reg, "session-1", `{"text":"found it in bar.go:42"}`)
	require.False(t, resp.IsError)

	require.Len(t, reg.sent, 1)
	require.Equal(t, "session-1", reg.sent[0].from)
	require.Equal(t, "found it in bar.go:42", reg.sent[0].text)
	require.Contains(t, reg.teamForCalls, "session-1")
}

func TestTeamReadRequiresASession(t *testing.T) {
	reg := &fakeTeamRegistry{}
	_, err := NewTeamReadTool(reg).Run(t.Context(), fantasy.ToolCall{ID: "call", Name: TeamReadToolName, Input: `{}`})
	require.Error(t, err)
}

func TestTeamReadRejectsNegativeWaitSeconds(t *testing.T) {
	reg := &fakeTeamRegistry{}
	resp := runTeamReadTool(t, reg, "session-1", `{"wait_seconds":-1}`)
	require.True(t, resp.IsError)
}

func TestTeamReadReportsNoNewMessages(t *testing.T) {
	reg := &fakeTeamRegistry{}
	resp := runTeamReadTool(t, reg, "session-1", `{}`)
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "No new team messages")
}

func TestTeamReadFormatsMessagesFromTheRegistry(t *testing.T) {
	reg := &fakeTeamRegistry{
		waitMsgs: []teams.Message{
			{Seq: 3, From: "session-2", Text: "already checked internal/foo", Time: time.Unix(0, 0).UTC()},
		},
	}
	resp := runTeamReadTool(t, reg, "session-1", `{"since":1}`)
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "session-2")
	require.Contains(t, resp.Content, "already checked internal/foo")
	require.Equal(t, 1, reg.waitSince)
}

func TestTeamReadCapsWaitSecondsAtTheMaximum(t *testing.T) {
	reg := &fakeTeamRegistry{}
	_ = runTeamReadTool(t, reg, "session-1", `{"wait_seconds":600}`)
	require.Equal(t, time.Duration(maxTeamWaitSeconds)*time.Second, reg.waitTimeout)
}

func TestTeamSendAndReadShareATeamAcrossDifferentSessions(t *testing.T) {
	reg := &fakeTeamRegistry{teamID: "root-session"}

	resp := runTeamSendTool(t, reg, "child-session", `{"text":"hello sibling"}`)
	require.False(t, resp.IsError)
	require.Equal(t, "root-session", reg.sent[0].teamID)

	_ = runTeamReadTool(t, reg, "sibling-session", `{}`)
	require.Contains(t, reg.teamForCalls, "sibling-session")
}

// The tools against the real registry: addressing, the cursor guard and status.
func TestTeamReadOnlyMineWaitsForTheMessageThatPagesYou(t *testing.T) {
	reg := teams.NewRegistry()
	reg.Join("lead", "rev")
	reg.SetName("lead", "lead")
	reg.SetName("rev", "reviewer")
	reg.Send("lead", "lead", "chatter for nobody")
	reg.Send("lead", "lead", "@reviewer please look at #1")

	ctx := context.WithValue(t.Context(), SessionIDContextKey, "rev")
	resp, err := NewTeamReadTool(reg).Run(ctx, fantasy.ToolCall{ID: "c", Name: TeamReadToolName, Input: `{"only_mine":true}`})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "@reviewer please look at #1")
	require.NotContains(t, resp.Content, "chatter for nobody")
	require.Contains(t, resp.Content, "lead (", "the sender is shown by handle")
	require.Contains(t, resp.Metadata, `"seq_after":2`)
}

func TestTeamReadRefusesACursorAheadOfTheBoard(t *testing.T) {
	reg := teams.NewRegistry()
	reg.Send("lead", "lead", "one")
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "lead")
	resp, err := NewTeamReadTool(reg).Run(ctx, fantasy.ToolCall{ID: "c", Name: TeamReadToolName, Input: `{"since":7}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "seq_after")
	require.Contains(t, resp.Metadata, `"seq_after":1`, "the error hands back the right cursor")
}

func TestTeamReadStatusShowsRosterAndStall(t *testing.T) {
	reg := teams.NewRegistry()
	reg.Join("lead", "dev")
	reg.SetName("lead", "lead")
	reg.SetName("dev", "dev")
	reg.Send("lead", "dev", "[blocked] need the staging key")

	ctx := context.WithValue(t.Context(), SessionIDContextKey, "lead")
	resp, err := NewTeamReadTool(reg).Run(ctx, fantasy.ToolCall{ID: "c", Name: TeamReadToolName, Input: `{"status":true}`})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "@lead: working")
	require.Contains(t, resp.Content, "@dev: working")
	require.Contains(t, resp.Content, "No stall found", "a fresh [blocked] post is not yet a stall")
}

func TestTeamStatusNeedsARegistryThatKnowsTheRoster(t *testing.T) {
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "s")
	resp, err := NewTeamReadTool(&fakeTeamRegistry{}).Run(ctx, fantasy.ToolCall{ID: "c", Name: TeamReadToolName, Input: `{"status":true}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
}
