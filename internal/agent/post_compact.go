package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/knowledge"
)

// afterCompaction runs once a conversation has been summarized: it fires the
// PostCompact hooks and, when project knowledge is on, proposes the summary
// as a knowledge item for the user to review. It never fails the compaction.
func (a *sessionAgent) afterCompaction(ctx context.Context, sessionID, summary string) {
	if a.isSubAgent {
		return
	}
	if a.invocationHooks != nil {
		if runners := a.invocationHooks.Get().v; runners != nil && runners.postCompact != nil {
			if _, err := runners.postCompact.RunSessionEvent(ctx, hooks.EventPostCompact, sessionID); err != nil {
				slog.Warn("PostCompact hook execution error", "session_id", sessionID, "error", err)
			}
		}
	}
	if a.taskContextConfig == nil || a.workingDir == "" {
		return
	}
	cfg := a.taskContextConfig.Config()
	if cfg == nil || cfg.Options == nil || !cfg.Options.Knowledge {
		return
	}
	store := knowledge.New(knowledge.DirFor(a.workingDir), a.workingDir)
	item, why, err := store.ProposeCheckpoint(summary, time.Now())
	switch {
	case err != nil:
		slog.Warn("Could not propose a knowledge item from the compaction summary", "session_id", sessionID, "error", err)
	case why != "":
		slog.Debug("No knowledge item proposed from the compaction summary", "session_id", sessionID, "reason", why)
	default:
		slog.Info("Proposed a knowledge item from the compaction summary; review it with `atlas knowledge`", "id", item.ID)
	}
}
