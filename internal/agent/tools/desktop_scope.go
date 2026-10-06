package tools

import (
	"context"
	"regexp"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

type desktopTaskScopeKey struct{}

var desktopTaskRequest = regexp.MustCompile(`(?is)computer[ _-]?use\s+(ile|kullanarak)|(?:using|with|through|via)\s+computer[ _-]?use|use\s+computer[ _-]?use\s+to|masaüstü.{0,35}(uygulama|üzerinden|kullanarak)|through the desktop|using the desktop|use the desktop application|in the desktop application|桌面应用.{0,20}(操作|完成)|приложени[ея].{0,30}рабочего стола`)

var desktopContinuation = regexp.MustCompile(`(?i)^(devam(?: et)?|continue|resume|go on|weiter|continuer|continua|继续)[.!\s]*$`)

// WithDesktopTaskScope captures task scope from the accepted user request.
// Inherited scope remains restrictive when specialists receive model prompts.
func WithDesktopTaskScope(ctx context.Context, prompt string) context.Context {
	if getContextValue(ctx, desktopTaskScopeKey{}, false) {
		return ctx
	}
	required := desktopTaskRequest.MatchString(prompt)
	if session := GetSessionFromContext(ctx); session != "" {
		state := desktopGuardFor(session)
		state.mu.Lock()
		if desktopContinuation.MatchString(strings.TrimSpace(prompt)) {
			required = state.guiOnly
		} else {
			state.guiOnly = required
		}
		state.mu.Unlock()
	}
	return context.WithValue(ctx, desktopTaskScopeKey{}, required)
}

// WithGUIOnlyDesktop explicitly binds scope for trusted runtime callers.
// This capability is not a model-controlled tool argument.
func WithGUIOnlyDesktop(ctx context.Context) context.Context {
	if session := GetSessionFromContext(ctx); session != "" {
		state := desktopGuardFor(session)
		state.mu.Lock()
		state.guiOnly = true
		state.mu.Unlock()
	}
	return context.WithValue(ctx, desktopTaskScopeKey{}, true)
}

type desktopScopeTool struct{ fantasy.AgentTool }

func (t *desktopScopeTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if resp, blocked := desktopScopeViolation(ctx, call.Name); blocked {
		return resp, nil
	}
	return t.AgentTool.Run(ctx, call)
}

func desktopScopeViolation(ctx context.Context, name string) (fantasy.ToolResponse, bool) {
	if !getContextValue(ctx, desktopTaskScopeKey{}, false) {
		return fantasy.ToolResponse{}, false
	}
	switch name {
	case BashToolName, EditToolName, WriteToolName, MultiEditToolName, "apply_patch", "execution":
		err := computer.NewContractError("task_scope_violation", "tool", "This task requires desktop application interaction; shell commands and direct file changes cannot substitute for it. Nothing executed", "Continue through computer/tool_pipeline desktop actions. If the desktop is blocked, report the blocker or use handoff.", `{"action":"windows"}`)
		return contractResponse(err), true
	}
	return fantasy.ToolResponse{}, false
}
