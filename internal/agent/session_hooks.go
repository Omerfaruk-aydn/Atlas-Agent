package agent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
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

// stopHookDefaultReason is the instruction a Stop hook's deny turns into when
// the hook gave no reason of its own.
const stopHookDefaultReason = "A Stop hook asked you to keep working. Re-read the request, finish what is still open, and report again."

// applyStopHooks runs Stop (or SubagentStop) hooks on a turn that ended on
// its own, after every completion gate has already passed. A deny decision
// turns the hook's reason into the agent's next instruction, up to
// hooks.MaxStopContinuations times per request.
//
// It only ever adds work. A turn a gate already sent back, left unresolved
// or marked unverified is returned unchanged and the hooks do not run: a
// script must not be able to wave an unfinished turn through, and there is
// nothing for it to veto when the gates have already refused. A halt
// decision is ignored too, because the turn is ending anyway. A hook that
// fails to run fails open for the same reason a failed prompt hook does.
//
// The second result reports whether the hooks sent the agent back to work.
func (a *sessionAgent) applyStopHooks(ctx context.Context, call SessionAgentCall, lastMessage string, evaluation evaluatorDecision) (evaluatorDecision, bool) {
	if evaluation.Continue != "" || evaluation.Unresolved != "" || evaluation.Unverified != "" {
		return evaluation, false
	}
	runner := a.stopHooks.Get().v
	if runner == nil {
		return evaluation, false
	}
	event := hooks.EventStop
	if a.isSubAgent {
		event = hooks.EventSubagentStop
	}
	result, err := runner.RunStop(ctx, event, call.SessionID, call.stopHookRounds > 0, lastMessage)
	if err != nil {
		slog.Warn("Stop hook execution error, ending the turn", "event", event, "session_id", call.SessionID, "error", err)
		return evaluation, false
	}
	if result.Decision != hooks.DecisionDeny {
		return evaluation, false
	}
	if call.stopHookRounds >= hooks.MaxStopContinuations {
		slog.Warn("Stop hooks asked to continue too many times; ending the turn",
			"event", event, "session_id", call.SessionID, "rounds", call.stopHookRounds)
		return evaluation, false
	}
	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = stopHookDefaultReason
	}
	a.sessionTrace(call.SessionID).record(harnessmetrics.TraceEvent{Kind: "continuation", Name: event, Reason: "stop_hook"})
	return evaluatorDecision{Continue: reason, stopHook: true}, true
}
