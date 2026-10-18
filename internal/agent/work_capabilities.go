package agent

import (
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// workCapabilityNames connects the chosen method to the existing tools.
// Only names from the live authorized palette are returned. It discovers
// schemas, never invokes tools or assigns specialists automatically.
func workCapabilityNames(st engineering.State, palette []fantasy.AgentTool) []string {
	if st.Work == nil || st.Work.Budget.Tier == engineering.TierDirect || st.Work.Phase == "waiting" {
		return nil
	}
	// A broad profile is only a hint. Until a method or investigation phase
	// has been chosen, keep ordinary tool discovery semantics intact.
	if len(st.Work.Methods) == 0 && st.Work.Phase != "investigate" && st.Work.Phase != "verify" {
		return nil
	}
	var names []string
	switch st.Profile {
	case "ui":
		names = append(names, "design_search", "design_system", "ui_verify", "a11y_audit", "interaction_audit")
	case "research":
		names = append(names, "web_search", "web_fetch", "source_memory")
	default:
		names = append(names, "context_select", "project_map", "impact_analysis")
	}
	if n := len(st.Work.Methods); n > 0 {
		switch st.Work.Methods[n-1].Method {
		case "experiment", "prototype":
			names = append(names, "bug_reproduce", "api_probe", "test_run", "benchmark_compare")
		case "candidates":
			names = append(names, "worktree", "verify", "tool_pipeline")
		case "delegate":
			names = append(names, "agent", "todos", "worktree")
		case "review":
			names = append(names, "verify", "test_select", "impact_analysis")
		case "research":
			names = append(names, "web_search", "source_memory")
		}
	}
	if st.Work.Phase == "verify" {
		names = append(names, "verify", "test_select", "lint_run")
	}
	out := paletteSubset(palette, names)
	slices.Sort(out)
	return slices.Compact(out)
}
