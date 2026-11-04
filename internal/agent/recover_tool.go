package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// recoverableTool turns an ordinary error returned by a tool into an error
// response the model can read. The agent loop treats every returned error as
// critical and ends the turn, so a refused file write, a DNS failure or a
// rejected plan update would otherwise stop the conversation instead of
// letting the model retry, narrow the call or take another route.
//
// Cancellation from above (the user pressing escape, shutdown) still
// propagates as an error: the run is over and nothing should continue it.
type recoverableTool struct {
	inner fantasy.AgentTool
}

// wrapToolsWithRecovery returns a tool slice with each entry wrapped in a
// recoverableTool.
func wrapToolsWithRecovery(agentTools []fantasy.AgentTool) []fantasy.AgentTool {
	out := make([]fantasy.AgentTool, len(agentTools))
	for i, tool := range agentTools {
		out[i] = &recoverableTool{inner: tool}
	}
	return out
}

func (t *recoverableTool) Info() fantasy.ToolInfo {
	return t.inner.Info()
}

func (t *recoverableTool) ProviderOptions() fantasy.ProviderOptions {
	return t.inner.ProviderOptions()
}

func (t *recoverableTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	t.inner.SetProviderOptions(opts)
}

func (t *recoverableTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	resp, err := t.inner.Run(ctx, call)
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return resp, err
	}
	slog.Warn("Tool returned an error; reporting it to the model", "tool", call.Name, "error", err)
	return fantasy.NewTextErrorResponse(fmt.Sprintf("Tool %q failed: %s", call.Name, err)), nil
}
