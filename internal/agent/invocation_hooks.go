package agent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
)

// invocationRunners holds the PreInvocation and PostInvocation hook runners.
type invocationRunners struct {
	pre, post, postCompact *hooks.Runner
}

// SetInvocationHooks updates the per-model-step hook runners in place, so a
// config edit reaches a running session.
func (a *sessionAgent) SetInvocationHooks(pre, post, postCompact *hooks.Runner) {
	a.invocationHooks.Set(ptrBox[invocationRunners]{v: &invocationRunners{pre: pre, post: post, postCompact: postCompact}})
}

// preInvocationDenied runs PreInvocation hooks before a model request of the
// top-level agent. A deny stops the request; a hook that fails to run does
// not, so a broken hook cannot lock the session.
func (a *sessionAgent) preInvocationDenied(ctx context.Context, sessionID string) (bool, string) {
	if a.isSubAgent || a.invocationHooks == nil {
		return false, ""
	}
	runners := a.invocationHooks.Get().v
	if runners == nil || runners.pre == nil {
		return false, ""
	}
	result, err := runners.pre.RunSessionEvent(ctx, hooks.EventPreInvocation, sessionID)
	if err != nil {
		slog.Warn("PreInvocation hook execution error, sending the request", "session_id", sessionID, "error", err)
		return false, ""
	}
	if result.Decision != hooks.DecisionDeny && !result.Halt {
		return false, ""
	}
	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = "no reason given"
	}
	return true, reason
}

// postInvocation tells PostInvocation hooks a model step finished. It is
// observational: the outcome is not used.
func (a *sessionAgent) postInvocation(ctx context.Context, sessionID string) {
	if a.isSubAgent || a.invocationHooks == nil {
		return
	}
	runners := a.invocationHooks.Get().v
	if runners == nil || runners.post == nil {
		return
	}
	if _, err := runners.post.RunSessionEvent(ctx, hooks.EventPostInvocation, sessionID); err != nil {
		slog.Warn("PostInvocation hook execution error", "session_id", sessionID, "error", err)
	}
}
