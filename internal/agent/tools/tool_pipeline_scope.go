package tools

import (
	"context"
	_ "embed"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

//go:embed tool_pipeline_browser.md
var pipelineBrowserTemplate string

var pipelineBrowserDescription = renderPipelineDoc("tool_pipeline_browser", pipelineBrowserTemplate)

// ToolPipelineName is the pipeline tool's name.
const ToolPipelineName = "tool_pipeline"

// browserScopedPipeline advertises tool_pipeline for browser work without its
// desktop recipes. The schema is a strict subset of the full one and Run is
// the same tool, so the model is offered nothing the runtime does not accept;
// it is shown when the computer tool is not loaded, where the desktop recipes
// would only cost request size.
type browserScopedPipeline struct {
	fantasy.AgentTool
}

func (p browserScopedPipeline) Info() fantasy.ToolInfo {
	info := p.AgentTool.Info()
	params := make(map[string]any, len(info.Parameters))
	for key, value := range info.Parameters {
		if key != "desktop" {
			params[key] = value
		}
	}
	info.Parameters = params
	info.Description = pipelineBrowserDescription
	return info
}

func (p browserScopedPipeline) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return p.AgentTool.Run(ctx, call)
}

// ScopePipelineForBrowser returns the palette with tool_pipeline in its
// browser-scoped form when the computer tool is not advertised. With the
// computer tool present the full pipeline, desktop recipes included, stays.
func ScopePipelineForBrowser(advertised []fantasy.AgentTool) []fantasy.AgentTool {
	computer, pipeline := false, -1
	for i, tool := range advertised {
		switch tool.Info().Name {
		case ComputerToolName:
			computer = true
		case ToolPipelineName:
			pipeline = i
		}
	}
	if computer || pipeline < 0 {
		return advertised
	}
	if _, scoped := advertised[pipeline].(browserScopedPipeline); scoped {
		return advertised
	}
	out := make([]fantasy.AgentTool, len(advertised))
	copy(out, advertised)
	out[pipeline] = browserScopedPipeline{AgentTool: advertised[pipeline]}
	return out
}
