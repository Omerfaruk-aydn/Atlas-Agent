package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/tidwall/sjson"
)

// hookedTool wraps a fantasy.AgentTool to run PreToolUse hooks before
// delegating to the inner tool, and PostToolUse hooks after it returns.
type hookedTool struct {
	inner fantasy.AgentTool
	pre   *hooks.Runner
	post  *hooks.Runner
}

func newHookedTool(inner fantasy.AgentTool, pre, post *hooks.Runner) *hookedTool {
	return &hookedTool{inner: inner, pre: pre, post: post}
}

// WithToolHooks applies the ordinary hook protocol to model-free tool runs.
func WithToolHooks(inner fantasy.AgentTool, pre, post *hooks.Runner) fantasy.AgentTool {
	return newHookedTool(inner, pre, post)
}

// wrapToolsWithHooks returns a tool slice with each entry wrapped in a
// hookedTool. Returns the original slice unchanged when runner is nil or
// when isSubAgent is true — sub-agents never fire hooks, the top-level
// invocation of the sub-agent tool itself is wrapped on the caller's side.
func wrapToolsWithHooks(tools []fantasy.AgentTool, pre, post *hooks.Runner, isSubAgent bool) []fantasy.AgentTool {
	if (pre == nil && post == nil) || isSubAgent {
		return tools
	}
	out := make([]fantasy.AgentTool, len(tools))
	for i, tool := range tools {
		out[i] = newHookedTool(tool, pre, post)
	}
	return out
}

func (h *hookedTool) Info() fantasy.ToolInfo {
	return h.inner.Info()
}

func (h *hookedTool) ProviderOptions() fantasy.ProviderOptions {
	return h.inner.ProviderOptions()
}

func (h *hookedTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	h.inner.SetProviderOptions(opts)
}

func (h *hookedTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	sessionID := tools.GetSessionFromContext(ctx)

	var result hooks.AggregateResult
	if h.pre != nil {
		var err error
		result, err = h.pre.Run(ctx, hooks.EventPreToolUse, sessionID, call.Name, call.Input)
		if err != nil {
			slog.Warn("Hook execution error, proceeding with tool call",
				"tool", call.Name, "error", err)
		}
	}

	if result.Decision == hooks.DecisionDeny || result.Halt {
		reason := fmt.Sprintf("Tool call blocked by hook. Reason: %s", result.Reason)
		if result.Halt {
			reason = fmt.Sprintf("Turn halted by hook. Reason: %s", result.Reason)
		}
		resp := fantasy.NewTextErrorResponse(reason)
		// Halt ends the whole turn; a plain deny only blocks this tool
		// call so the model can see the error and try something else.
		resp.StopTurn = result.Halt
		resp.Metadata = hookMetadataJSON(result)
		return resp, nil
	}

	if result.UpdatedInput != "" {
		call.Input = result.UpdatedInput
	}

	// An explicit allow from a hook pre-approves the permission prompt for
	// this tool call. Deny is already handled above; silence falls through
	// to the normal permission flow.
	if result.Decision == hooks.DecisionAllow {
		ctx = permission.WithHookApproval(ctx, call.ID)
	}

	resp, err := h.inner.Run(ctx, call)
	if err != nil {
		return resp, err
	}

	if result.Context != "" {
		if resp.Content != "" {
			resp.Content += "\n"
		}
		resp.Content += result.Context
	}

	resp.Metadata = mergeHookMetadata(resp.Metadata, result)
	return h.runPostHooks(ctx, sessionID, call, resp)
}

// runPostHooks fires PostToolUse and folds its outcome into the response.
// The call already happened, so a deny cannot undo it -- what a post hook
// can do is add context the model will see, or halt the turn.
func (h *hookedTool) runPostHooks(ctx context.Context, sessionID string, call fantasy.ToolCall, resp fantasy.ToolResponse) (fantasy.ToolResponse, error) {
	if h.post == nil {
		return resp, nil
	}

	result, err := h.post.RunPost(ctx, sessionID, call.Name, call.Input, resp.Content)
	if err != nil {
		slog.Warn("PostToolUse hook execution error, keeping the tool result",
			"tool", call.Name, "error", err)
		return resp, nil
	}

	if result.Context != "" {
		if resp.Content != "" {
			resp.Content += "\n"
		}
		resp.Content += result.Context
	}
	if result.Halt {
		resp.StopTurn = true
		if result.Reason != "" {
			resp.Content += "\n\nTurn halted by hook. Reason: " + result.Reason
		}
	}
	resp.Metadata = mergeHookMetadata(resp.Metadata, result)
	return resp, nil
}

// buildHookMetadata creates a HookMetadata from an AggregateResult.
func buildHookMetadata(result hooks.AggregateResult) hooks.HookMetadata {
	return hooks.HookMetadata{
		HookCount:    result.HookCount,
		Decision:     result.Decision.String(),
		Halt:         result.Halt,
		Reason:       result.Reason,
		InputRewrite: result.UpdatedInput != "",
		Hooks:        result.Hooks,
	}
}

// hookMetadataJSON builds a JSON string containing only the hook metadata.
func hookMetadataJSON(result hooks.AggregateResult) string {
	meta := buildHookMetadata(result)
	data, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return `{"hook":` + string(data) + `}`
}

// mergeHookMetadata injects hook metadata into existing tool metadata.
func mergeHookMetadata(existing string, result hooks.AggregateResult) string {
	if result.HookCount == 0 {
		return existing
	}
	meta := buildHookMetadata(result)
	data, err := json.Marshal(meta)
	if err != nil {
		return existing
	}
	if existing == "" {
		existing = "{}"
	}
	merged, err := sjson.SetRaw(existing, "hook", string(data))
	if err != nil {
		return existing
	}
	return merged
}
