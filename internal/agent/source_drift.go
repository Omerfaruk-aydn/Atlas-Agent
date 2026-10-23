package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// noteSourceDrift compares the project tree with the fingerprint recorded
// after the last passing check. It exposes changes no tracked tool reported (a
// background job still writing, an edit made outside the agent, a partial
// write left by a failed call) by advancing the source generation, which
// stales every earlier check. It runs where evidence is about to be relied on,
// not on every tool call, because fingerprinting reads the tree. A tree that
// cannot be read counts as changed.
func noteSourceDrift(ctx context.Context, store *engineering.Store, root, sessionID string) bool {
	if store == nil || root == "" {
		return false
	}
	scope := engineering.GetScope(ctx, sessionID).SessionID
	if _, ok := store.SourceMarked(ctx, scope); !ok {
		return false
	}
	snapshot, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	current, err := engineering.ProjectFingerprint(snapshot, root, store.Dir())
	if err != nil {
		current = "unreadable: " + err.Error()
	}
	drifted, err := store.NoteSourceDrift(snapshot, scope, current)
	if err != nil {
		slog.Warn("Could not record source drift", "session_id", scope, "error", err)
		return false
	}
	if drifted {
		slog.Warn("Project files changed since the last passing check without a tracked edit; earlier checks are stale", "session_id", scope)
	}
	return drifted
}

func (a *sessionAgent) checkSourceDrift(ctx context.Context, sessionID string) bool {
	return noteSourceDrift(ctx, a.engineering, a.projectRoot(sessionID), sessionID)
}
