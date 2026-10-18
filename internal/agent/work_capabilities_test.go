package agent

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestWorkCapabilitySelectionRespectsPaletteAndMethod(t *testing.T) {
	t.Parallel()
	var palette []fantasy.AgentTool
	for _, name := range []string{"project_map", "impact_analysis", "worktree", "verify", "tool_pipeline", "design_search"} {
		palette = append(palette, fantasy.NewAgentTool(name, name, func(context.Context, probeInput, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("unused"), nil
		}))
	}
	st := engineering.State{Profile: "feature", Work: &engineering.WorkState{Budget: engineering.WorkBudget{Tier: engineering.TierDeep}, Methods: []engineering.MethodDecision{{Method: "candidates"}}}}
	names := workCapabilityNames(st, palette)
	require.Contains(t, names, "worktree")
	require.Contains(t, names, "verify")
	require.NotContains(t, names, "agent")
	require.NotContains(t, names, "context_select", "disabled tools must not be resurrected")
	st.Profile = "ui"
	st.Work.Methods = nil
	require.Empty(t, workCapabilityNames(st, palette))
	st.Work.Phase = "investigate"
	require.Equal(t, []string{"design_search"}, workCapabilityNames(st, palette))
	st.Work.Budget.Tier = engineering.TierDirect
	require.Empty(t, workCapabilityNames(st, palette))
	st.Work.Budget.Tier = engineering.TierDeep
	st.Work.Phase = "waiting"
	require.Empty(t, workCapabilityNames(st, palette))
}
