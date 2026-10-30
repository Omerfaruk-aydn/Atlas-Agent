package agent

import (
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

// Phase routing is opt-in through exploration, planning, implementation,
// verification and review model roles. The phase is inferred from completed
// tool work; a role change happens only before a new model step, never while a
// request or tool call is in flight.
func workPhase(messages fantasy.Prompt) string {
	phase := "planning"
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != fantasy.MessageRoleAssistant {
			continue
		}
		for _, part := range messages[i].Content {
			call, ok := part.(fantasy.ToolCallPart)
			if !ok {
				continue
			}
			name := strings.ToLower(call.ToolName)
			switch {
			case slices.Contains([]string{"edit", "write", "multiedit", "replace_symbol", "rename_symbol"}, name):
				return "implementation"
			case slices.Contains([]string{"bash", "joboutput", "verify", "test_run", "lint_run"}, name):
				phase = "verification"
			case slices.Contains([]string{"view", "grep", "glob", "ls", "read", "code_query", "project_map"}, name) && phase == "planning":
				phase = "exploration"
			}
		}
		break
	}
	return phase
}

func sameModel(a, b Model) bool {
	return a.ModelCfg.Provider == b.ModelCfg.Provider && a.ModelCfg.Model == b.ModelCfg.Model && a.ModelCfg.ReasoningEffort == b.ModelCfg.ReasoningEffort
}

// selectWorkPhase switches at most once per step boundary and records the
// reason so cache cost can be compared with the existing prompt_cache events.
func (a *sessionAgent) selectWorkPhase(chain *modelChain, primary Model, messages fantasy.Prompt, sessionID string) {
	if a.stageModels == nil {
		return
	}
	phase := workPhase(messages)
	selected := a.stageModels(phase)
	if selected == nil {
		selected = &primary
	}
	if sameModel(chain.Current(), *selected) {
		return
	}
	chain.RoutePhase(*selected)
	a.recordHarness(harnessmetrics.ModelStage, sessionID, map[string]string{"phase": phase, "provider": selected.ModelCfg.Provider, "model": selected.ModelCfg.Model})
}
