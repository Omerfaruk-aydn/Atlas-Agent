package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

// BrowserRegressionSessions is the existing browser session provider seam.
// A regression run owns its provider and must close it when the run ends.
type BrowserRegressionSessions interface {
	Session(id string) (browser.Session, error)
	Close(id string)
}

// BrowserRegressionResult is the complete ledger produced by the ordinary
// browser workflow pipeline. Callers must evaluate every required step; a
// successful recovery branch never makes a failed required assertion pass.
type BrowserRegressionResult struct {
	Steps []PipelineResult `json:"steps"`
}

// CompileBrowserRegressionWorkflow exposes the existing compiler so the
// runner can require exactly the generated action and verification steps.
func CompileBrowserRegressionWorkflow(workflow BrowserWorkflowParams) ([]PipelineStep, error) {
	return compileBrowserWorkflow(workflow)
}

// RunBrowserRegression executes a fixed workflow without a model. It is a
// thin adapter over compileBrowserWorkflow and runToolPipeline, not another
// browser engine. The synthetic session and permissions belong only to the
// isolated regression browser supplied by the caller.
func RunBrowserRegression(ctx context.Context, sessions BrowserRegressionSessions, workingDir, sessionID string, workflow BrowserWorkflowParams) (BrowserRegressionResult, error) {
	var result BrowserRegressionResult
	if sessions == nil || workingDir == "" || sessionID == "" {
		return result, fmt.Errorf("browser regression requires isolated sessions, working directory and session ID")
	}
	permissions := permission.NewPermissionService(workingDir, true, nil)
	browserTool := newBrowserTool(permissions, workingDir, sessions, browserDescription(false))
	tool := NewToolPipeline(func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return browserTool.Run(ctx, call)
	})
	ctx = context.WithValue(ctx, SessionIDContextKey, sessionID)
	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "web-regression", Name: "tool_pipeline", Input: mustBrowserRegressionInput(workflow)})
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(response.Content), &result.Steps); err != nil {
		content := response.Content
		if len(content) > 512 {
			content = content[:512]
		}
		return result, fmt.Errorf("browser regression returned an invalid step ledger: %w; response=%q", err, content)
	}
	if len(result.Steps) == 0 {
		return result, fmt.Errorf("browser regression returned no steps")
	}
	return result, nil
}

func mustBrowserRegressionInput(workflow BrowserWorkflowParams) string {
	encoded, err := json.Marshal(PipelineParams{Browser: &workflow, TimeoutMS: 90000})
	if err != nil {
		// BrowserWorkflowParams contains only JSON-safe values. Keep the tool
		// call construction in one place without hiding a future schema error.
		panic(err)
	}
	return string(encoded)
}
