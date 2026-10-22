package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

const oracleMaxCallsPerHour = 3

type oracleBudget struct {
	mu    sync.Mutex
	start time.Time
	calls int
}

func (c *coordinator) reserveOracle(sessionID string, now time.Time) bool {
	value, _ := c.oracleBudgets.LoadOrStore(sessionID, &oracleBudget{})
	budget := value.(*oracleBudget)
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.start.IsZero() || now.Sub(budget.start) >= time.Hour {
		budget.start, budget.calls = now, 0
	}
	if budget.calls >= oracleMaxCallsPerHour {
		return false
	}
	budget.calls++
	return true
}

func (c *coordinator) oracleTool() fantasy.AgentTool {
	type params struct {
		Problem string   `json:"problem" jsonschema:"required,description=The concrete blocking problem"`
		Context string   `json:"context" jsonschema:"required,description=Observed evidence and relevant constraints"`
		Options []string `json:"options" jsonschema:"description=Approaches already considered, at most 6"`
	}
	return fantasy.NewAgentTool("oracle", "Consult a stronger read-only model about a concrete blocking problem. Provide evidence and options; verify its advice before acting. At most 3 calls per session per hour.", func(ctx context.Context, p params, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if strings.TrimSpace(p.Problem) == "" || strings.TrimSpace(p.Context) == "" || len(p.Problem) > 6000 || len(p.Context) > 12000 || len(p.Options) > 6 {
			return fantasy.NewTextErrorResponse("oracle requires a bounded problem and evidence context, with at most 6 options"), nil
		}
		sessionID := tools.GetSessionFromContext(ctx)
		if !c.reserveOracle(sessionID, time.Now()) {
			return fantasy.NewTextErrorResponse("oracle hourly call budget exhausted; continue with local evidence or ask the user"), nil
		}
		worker, ok := c.currentAgent.(*sessionAgent)
		if !ok {
			return fantasy.NewTextErrorResponse("oracle is unavailable in this session"), nil
		}
		roleName := "oracle"
		role, ok := c.cfg.Config().ResolveRole(roleName)
		if !ok {
			roleName = "escalate"
			role, ok = c.cfg.Config().ResolveRole(roleName)
		}
		if !ok {
			return fantasy.NewTextErrorResponse("no oracle or escalate model role is configured"), nil
		}
		model, err := c.resolveModel(ctx, role, true)
		if err != nil {
			return fantasy.NewTextErrorResponse("oracle model is unavailable: " + err.Error()), nil
		}
		reviewTools, err := c.buildTools(ctx, config.Agent{AllowedTools: advisorToolNames}, true)
		if err != nil {
			return fantasy.NewTextErrorResponse("oracle read-only tools are unavailable: " + err.Error()), nil
		}
		prompt := fmt.Sprintf("Problem:\n%s\n\nObserved context:\n%s\n\nOptions considered:\n%s", p.Problem, p.Context, strings.Join(p.Options, "\n- "))
		advice, err := worker.runReviewer(ctx, sessionID, model, "You are a read-only troubleshooting oracle. Give a bounded diagnosis and one discriminating experiment. Do not edit files, run commands, or claim verification you did not perform.", prompt, nil, reviewTools, 8)
		if err != nil {
			return fantasy.NewTextErrorResponse("oracle consultation failed: " + err.Error()), nil
		}
		worker.recordHarness(harnessmetrics.OracleConsulted, sessionID, map[string]string{"role": roleName, "provider": model.ModelCfg.Provider})
		return fantasy.NewTextResponse(advice), nil
	})
}
