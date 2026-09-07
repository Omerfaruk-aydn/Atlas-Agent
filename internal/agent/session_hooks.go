package agent

import (
	"context"
	"log/slog"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
)

// fireSessionStart runs EventSessionStart hooks the first time this
// process handles a turn for sessionID, and never again for it. A hook
// failure is logged and otherwise ignored: a broken script in someone's
// config should not stop a turn from running, the same posture
// applyPromptHooks takes toward a failed UserPromptSubmit hook.
//
// Unlike applyPromptHooks, a SessionStart hook cannot refuse the turn --
// there is no prompt yet to refuse at this point in a resumed session, and
// the event exists for setup/context, not approval -- so only its context
// is used, appended ahead of the prompt the same way a prompt hook's
// context is appended after it (see appendPromptContext); putting session
// context first keeps it readable as "here's the situation" before "here's
// what the user asked."
func (a *sessionAgent) fireSessionStart(ctx context.Context, sessionID, prompt string) string {
	sessionStartHooks := a.sessionStartHooks.Get().v
	if sessionStartHooks == nil {
		return prompt
	}
	if _, seen := a.startedSessions.Get(sessionID); seen {
		return prompt
	}
	a.startedSessions.Set(sessionID, true)

	result, err := sessionStartHooks.RunSessionEvent(ctx, hooks.EventSessionStart, sessionID)
	if err != nil {
		slog.Warn("SessionStart hook execution error", "session_id", sessionID, "error", err)
		return prompt
	}
	if result.Context == "" {
		return prompt
	}
	if prompt == "" {
		return result.Context
	}
	return result.Context + "\n\n" + prompt
}

// preCompactDenied runs EventPreCompact hooks and reports whether
// summarization should be skipped for this turn. A hook execution error
// fails open (compaction proceeds) for the same reason a failed prompt
// hook fails open: a broken script should not change the agent's behavior
// by accident.
func (a *sessionAgent) preCompactDenied(ctx context.Context, sessionID string) bool {
	preCompactHooks := a.preCompactHooks.Get().v
	if preCompactHooks == nil {
		return false
	}
	result, err := preCompactHooks.RunSessionEvent(ctx, hooks.EventPreCompact, sessionID)
	if err != nil {
		slog.Warn("PreCompact hook execution error, compacting anyway", "session_id", sessionID, "error", err)
		return false
	}
	if result.Decision == hooks.DecisionDeny || result.Halt {
		slog.Info("PreCompact hook denied compaction; turn continues over budget",
			"session_id", sessionID, "reason", result.Reason)
		return true
	}
	return false
}
