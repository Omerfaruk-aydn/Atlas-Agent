package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
)

// applyPromptHooks fires UserPromptSubmit hooks and returns the prompt the
// turn should actually run with. A hook can refuse the prompt outright, or
// hand back context that is appended to it.
//
// A hook that fails to run is not allowed to block the turn: a broken
// script in someone's config should not make the agent unusable, so the
// error is logged and the prompt goes through unchanged.
func (a *sessionAgent) applyPromptHooks(ctx context.Context, call SessionAgentCall) (string, error) {
	promptHooks := a.promptHooks.Get().v
	if promptHooks == nil {
		return call.Prompt, nil
	}

	result, err := promptHooks.RunPrompt(ctx, call.SessionID, call.Prompt)
	if err != nil {
		slog.Warn("UserPromptSubmit hook execution error, running the prompt unchanged",
			"session_id", call.SessionID, "error", err)
		return call.Prompt, nil
	}

	if result.Decision == hooks.DecisionDeny || result.Halt {
		if result.Reason == "" {
			return "", ErrPromptBlockedByHook
		}
		return "", fmt.Errorf("%w: %s", ErrPromptBlockedByHook, result.Reason)
	}

	return appendPromptContext(call.Prompt, result.Context), nil
}

// appendPromptContext folds a hook's context into the prompt. It goes after
// the prompt rather than before so the user's own words stay the first
// thing the model reads.
func appendPromptContext(prompt, context string) string {
	if strings.TrimSpace(context) == "" {
		return prompt
	}
	if prompt == "" {
		return context
	}
	return prompt + "\n\n" + context
}
