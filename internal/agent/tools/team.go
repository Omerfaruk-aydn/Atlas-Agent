package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/teams"
)

const (
	TeamSendToolName = "team_send"
	TeamReadToolName = "team_read"
)

//go:embed team_send.md
var teamSendDescription string

//go:embed team_read.md
var teamReadDescription string

// maxTeamWaitSeconds caps team_read's wait_seconds so a single call cannot
// block a turn indefinitely.
const maxTeamWaitSeconds = 60

// teamRegistry is the seam the team tools depend on instead of
// *teams.Registry directly, so tests can supply a fake without relying on
// the real registry's timing behavior.
type teamRegistry interface {
	TeamFor(sessionID string) string
	Send(teamID, from, text string) teams.Message
	Wait(ctx context.Context, teamID string, since int, timeout time.Duration) []teams.Message
}

// boardRegistry is the part of the registry that knows about addressing,
// cursors and roster. The team tools use it when the registry offers it and
// fall back to plain Send/Wait otherwise, so a simple registry still works.
type boardRegistry interface {
	ReadFor(ctx context.Context, teamID, sessionID string, since int, onlyMine bool, timeout time.Duration) (teams.Read, error)
	Snapshot(teamID string) teams.Board
}

type TeamSendParams struct {
	Text string `json:"text" description:"Message for this task's team: the session that started the task, plus every sub-agent it has spawned, however deeply nested. Address it with @name (one member), @all (everyone) or @#channel; a message with no @ pages nobody and is only seen by those who read the whole board."`
}

type TeamReadParams struct {
	Since       int  `json:"since,omitempty" description:"Only return messages with a sequence number greater than this. Pass the seq_after value a previous team_send/team_read call returned, never the newest message's number you saw elsewhere: a cursor ahead of the board is refused."`
	WaitSeconds int  `json:"wait_seconds,omitempty" description:"If nothing is available yet, wait up to this many seconds (max 60) for a message to arrive instead of returning empty right away. Defaults to 0: return immediately."`
	OnlyMine    bool `json:"only_mine,omitempty" description:"Return only messages addressed to you (@your-name, @all, or a channel you are in). Use it with wait_seconds to park until someone needs you, without being woken by traffic meant for others."`
	Status      bool `json:"status,omitempty" description:"Instead of messages, return the team roster (who is working, waiting or done) and any stall the board shows, such as everyone waiting, an unanswered @mention or a loop between two members. Each finding cites the message it comes from."`
}

type TeamResponseMetadata struct {
	Action   string `json:"action"`
	SeqAfter int    `json:"seq_after"`
}

func NewTeamSendTool(registry teamRegistry) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TeamSendToolName,
		teamSendDescription,
		func(ctx context.Context, params TeamSendParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			text := strings.TrimSpace(params.Text)
			if text == "" {
				return fantasy.NewTextErrorResponse("text is required"), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for team_send")
			}

			teamID := registry.TeamFor(sessionID)
			msg := registry.Send(teamID, sessionID, text)

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Sent (seq %d). Use team_read with since=%d to see replies.", msg.Seq, msg.Seq)),
				TeamResponseMetadata{Action: "send", SeqAfter: msg.Seq},
			), nil
		},
	)
}

func NewTeamReadTool(registry teamRegistry) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TeamReadToolName,
		teamReadDescription,
		func(ctx context.Context, params TeamReadParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for team_read")
			}
			if params.WaitSeconds < 0 {
				return fantasy.NewTextErrorResponse("wait_seconds must not be negative"), nil
			}

			wait := params.WaitSeconds
			if wait > maxTeamWaitSeconds {
				wait = maxTeamWaitSeconds
			}

			teamID := registry.TeamFor(sessionID)
			board, hasBoard := registry.(boardRegistry)
			if params.Status {
				if !hasBoard {
					return fantasy.NewTextErrorResponse("this team has no roster to report"), nil
				}
				return fantasy.NewTextResponse(formatTeamStatus(board.Snapshot(teamID))), nil
			}

			var msgs []teams.Message
			seqAfter := params.Since
			if hasBoard {
				read, err := board.ReadFor(ctx, teamID, sessionID, params.Since, params.OnlyMine, time.Duration(wait)*time.Second)
				if err != nil {
					if errors.Is(err, teams.ErrSinceAhead) {
						return fantasy.WithResponseMetadata(
							fantasy.NewTextErrorResponse(err.Error()),
							TeamResponseMetadata{Action: "read", SeqAfter: read.SeqAfter},
						), nil
					}
					return fantasy.ToolResponse{}, err
				}
				msgs, seqAfter = read.Messages, read.SeqAfter
			} else {
				msgs = registry.Wait(ctx, teamID, params.Since, time.Duration(wait)*time.Second)
				if len(msgs) > 0 {
					seqAfter = msgs[len(msgs)-1].Seq
				}
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(formatTeamMessages(msgs)),
				TeamResponseMetadata{Action: "read", SeqAfter: seqAfter},
			), nil
		},
	)
}

func formatTeamMessages(msgs []teams.Message) string {
	if len(msgs) == 0 {
		return "No new team messages."
	}
	var b strings.Builder
	for _, m := range msgs {
		from := m.From
		if m.Name != "" {
			from = m.Name
		}
		fmt.Fprintf(&b, "[seq %d] %s (%s): %s\n", m.Seq, from, m.Time.Format(time.RFC3339), m.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatTeamStatus renders the roster and what Diagnose finds. Findings cite
// a message number or a member, so the reader can check them against the board.
func formatTeamStatus(b teams.Board) string {
	var out strings.Builder
	out.WriteString("Team roster:\n")
	for _, m := range b.Members {
		fmt.Fprintf(&out, "- @%s: %s for %s", m.Name, m.State, b.Now.Sub(m.Since).Round(time.Second))
		if len(m.Channels) > 0 {
			fmt.Fprintf(&out, " (channels: #%s)", strings.Join(m.Channels, ", #"))
		}
		out.WriteByte('\n')
	}
	if len(b.Members) == 0 {
		out.WriteString("- nobody has joined yet\n")
	}
	alerts := teams.Diagnose(b)
	if len(alerts) == 0 {
		out.WriteString("\nNo stall found.")
		return out.String()
	}
	out.WriteString("\nStall findings, most urgent first:\n")
	for i, a := range alerts {
		fmt.Fprintf(&out, "%d. [%s] %s\n", i+1, a.Rule, a.Text)
	}
	return strings.TrimRight(out.String(), "\n")
}
