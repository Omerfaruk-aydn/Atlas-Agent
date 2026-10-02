package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

//go:embed usage.md
var usageDescription string

const UsageToolName = "usage"

type UsageParams struct{}

type UsageResponseMetadata struct {
	RuntimeUsage     *engineering.Usage  `json:"runtime_usage,omitempty"`
	RuntimeLimits    *engineering.Limits `json:"runtime_limits,omitempty"`
	PromptTokens     int64               `json:"prompt_tokens"`
	CompletionTokens int64               `json:"completion_tokens"`
	Cost             float64             `json:"cost"`
	// MaxSessionCost and Remaining are zero when no budget is configured.
	MaxSessionCost float64 `json:"max_session_cost,omitempty"`
	Remaining      float64 `json:"remaining,omitempty"`
}

// NewUsageTool lets the agent read its own session's spend.
//
// It is read-only and asks nothing of the user: the numbers it reports are
// already visible in the sidebar, this just puts them somewhere the model
// can act on. maxSessionCost is the same budget agent.Options.MaxSessionCost
// enforces (0 means none configured) -- reported here so the model can see
// a hard stop coming instead of only discovering it when a prompt is
// refused.
func NewUsageTool(sessions session.Service, maxSessionCost float64, runtime ...*engineering.Store) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		UsageToolName,
		usageDescription,
		func(ctx context.Context, params UsageParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session_id is required")
			}

			sess, err := sessions.Get(ctx, sessionID)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("reading session: %w", err)
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d prompt tokens, %d completion tokens, $%.4f spent so far this session.\n",
				sess.PromptTokens, sess.CompletionTokens, sess.Cost)

			meta := UsageResponseMetadata{
				PromptTokens:     sess.PromptTokens,
				CompletionTokens: sess.CompletionTokens,
				Cost:             sess.Cost,
			}
			if maxSessionCost > 0 {
				remaining := maxSessionCost - sess.Cost
				meta.MaxSessionCost = maxSessionCost
				meta.Remaining = remaining
				if remaining > 0 {
					fmt.Fprintf(&b, "Budget: $%.4f of $%.4f remaining.\n", remaining, maxSessionCost)
				} else {
					b.WriteString("Budget: exhausted. The next prompt in this session will be refused.\n")
				}
			}

			if len(runtime) > 0 && runtime[0] != nil {
				scope := engineering.GetScope(ctx, sessionID)
				state, err := runtime[0].Read(ctx, scope.SessionID)
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				meta.RuntimeUsage, meta.RuntimeLimits = &state.Usage, &state.Limits
				fmt.Fprintf(&b, "Cumulative runtime: %d tokens, %d tool calls, %d active ms, $%.4f (includes delegated and auxiliary calls). Inspect workflow status for task budgets.\n", state.Usage.Tokens, state.Usage.ToolCalls, state.Usage.DurationMS, state.Usage.Cost)
			}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), meta), nil
		},
	)
}
