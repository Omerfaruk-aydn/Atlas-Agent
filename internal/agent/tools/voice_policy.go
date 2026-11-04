package tools

import (
	"context"
	"log/slog"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

type voicePolicyTool struct{ fantasy.AgentTool }

// WithVoicePolicy applies the phone-call tool policy to a tool. Outside a
// voice turn it does nothing. During one, blocked tools are refused,
// confirmation tools are put to the caller first, and only an explicit
// spoken yes pre-approves that single call, the same way an allowing
// PreToolUse hook does. Everything else still goes through the normal
// permission service, which runs without prompts during a call.
func WithVoicePolicy(tool fantasy.AgentTool) fantasy.AgentTool { return &voicePolicyTool{tool} }

func (t *voicePolicyTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	turn, ok := voice.TurnFrom(ctx)
	if !ok {
		return t.AgentTool.Run(ctx, call)
	}
	auth, err := turn.Authorize(ctx, call.Name, call.Input)
	if err != nil && ctx.Err() != nil {
		return fantasy.ToolResponse{}, ctx.Err()
	}
	if !auth.Run {
		slog.Info("Voice policy refused tool call", "call_id", turn.CallID, "turn_id", turn.TurnID, "tool", call.Name)
		return fantasy.NewTextErrorResponse(auth.Reason), nil
	}
	if auth.Confirmed {
		ctx = permission.WithHookApproval(ctx, call.ID)
	}
	return t.AgentTool.Run(ctx, call)
}
