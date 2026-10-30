package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

// idleCompactionDue is deliberately based on observed session activity and
// token usage, not wall-clock process idleness. A child session is managed by
// its parent's agent and must not be compacted independently.
func idleCompactionDue(s session.Session, now time.Time, idle time.Duration, tokens int64) bool {
	return s.ParentSessionID == "" && tokens > 0 && s.PromptTokens+s.CompletionTokens >= tokens &&
		s.UpdatedAt > 0 && now.Sub(time.Unix(s.UpdatedAt, 0)) >= idle
}

func (c *coordinator) idleCompactionTick(ctx context.Context) {
	if c.currentAgent == nil || c.cfg == nil || c.cfg.Config() == nil || c.cfg.Config().Options == nil {
		return
	}
	idle, tokens, enabled := c.cfg.Config().Options.Harness.IdleCompactionPolicy()
	if !enabled || c.currentAgent.IsBusy() {
		return
	}
	sessions, err := c.sessions.List(ctx)
	if err != nil {
		slog.Warn("Failed to list sessions for idle compaction", "error", err)
		return
	}
	now := time.Now()
	for _, s := range sessions {
		if !idleCompactionDue(s, now, idle, tokens) || c.currentAgent.IsSessionBusy(s.ID) || c.currentAgent.QueuedPrompts(s.ID) > 0 {
			continue
		}
		// Summarize rechecks busyness and owns a cancellable active request.
		// A user message either waits for the atomic summary or cancels it;
		// no partial summary is installed into the session.
		if err := c.currentAgent.Summarize(ctx, s.ID, nil, nil); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrSessionBusy) {
			slog.Warn("Idle session compaction failed", "session_id", s.ID, "error", err)
		}
		return // At most one paid background compaction per scheduler tick.
	}
}
