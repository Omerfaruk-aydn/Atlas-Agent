package tools

import (
	"context"
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

//go:embed session_read.md
var sessionReadDescription string

const SessionReadToolName = "session_read"

const (
	// recentSessionCount is how many sessions the index lists.
	recentSessionCount = 8
	// sessionReadDefaultLimit and sessionReadMaxLimit bound one page of
	// messages.
	sessionReadDefaultLimit = 12
	sessionReadMaxLimit     = 40
	// sessionReadMessageChars cuts each message so one page stays small.
	sessionReadMessageChars = 1500
)

type SessionReadParams struct {
	SessionID string `json:"session_id,omitempty" description:"The session to read, as named by session_search or the recent-sessions list. Omit to list the most recent sessions instead."`
	Offset    int    `json:"offset,omitempty" description:"Index of the first message to return (0-based). Use the next_offset in the previous page's footer to continue."`
	Limit     int    `json:"limit,omitempty" description:"Messages per page. Defaults to 12, at most 40."`
}

// NewSessionReadTool lets the agent read an earlier session in bounded
// pages, or list the most recent ones. session_search finds the session;
// this reads it.
func NewSessionReadTool(sessions session.Service, messages message.Service, currentSession func(context.Context) string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SessionReadToolName,
		sessionReadDescription,
		func(ctx context.Context, params SessionReadParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if strings.TrimSpace(params.SessionID) == "" {
				return listRecentSessions(ctx, sessions, currentSession(ctx))
			}
			if _, err := sessions.Get(ctx, params.SessionID); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("No session %q. Use session_search or call session_read without a session_id to list recent sessions.", params.SessionID)), nil
			}
			msgs, err := messages.List(ctx, params.SessionID)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("reading session: %w", err)
			}
			return fantasy.NewTextResponse(formatSessionPage(msgs, params.Offset, params.Limit)), nil
		},
	)
}

func listRecentSessions(ctx context.Context, sessions session.Service, current string) (fantasy.ToolResponse, error) {
	all, err := sessions.List(ctx)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("listing sessions: %w", err)
	}
	recent := make([]session.Session, 0, len(all))
	for _, s := range all {
		if s.ID != current && s.ParentSessionID == "" {
			recent = append(recent, s)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].UpdatedAt > recent[j].UpdatedAt })
	if len(recent) > recentSessionCount {
		recent = recent[:recentSessionCount]
	}
	if len(recent) == 0 {
		return fantasy.NewTextResponse("There are no earlier sessions in this workspace."), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d most recent earlier sessions. Read one with session_read(session_id).\n", len(recent))
	for _, s := range recent {
		title := s.Title
		if title == "" {
			title = "(untitled session)"
		}
		fmt.Fprintf(&b, "\n- %s  %q  %s  %d messages", s.ID, title, time.Unix(s.UpdatedAt, 0).Format(time.DateOnly), s.MessageCount)
	}
	return fantasy.NewTextResponse(b.String()), nil
}

// formatSessionPage renders one bounded page of a conversation.
func formatSessionPage(msgs []message.Message, offset, limit int) string {
	if limit <= 0 {
		limit = sessionReadDefaultLimit
	}
	limit = min(limit, sessionReadMaxLimit)
	offset = max(offset, 0)
	if offset >= len(msgs) {
		return fmt.Sprintf("The session has %d messages; offset %d is past the end.", len(msgs), offset)
	}
	end := min(offset+limit, len(msgs))
	var b strings.Builder
	fmt.Fprintf(&b, "Messages %d-%d of %d.\n", offset, end-1, len(msgs))
	for i := offset; i < end; i++ {
		m := msgs[i]
		text := strings.TrimSpace(m.Content().Text)
		if text == "" {
			text = "(no text; tool activity only)"
		}
		if r := []rune(text); len(r) > sessionReadMessageChars {
			text = string(r[:sessionReadMessageChars]) + " [cut]"
		}
		fmt.Fprintf(&b, "\n[%d] %s:\n%s\n", i, m.Role, text)
	}
	if end < len(msgs) {
		fmt.Fprintf(&b, "\nnext_offset: %d", end)
	}
	return b.String()
}
