package tools

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"text/template"
	"time"
)

//go:embed tool_pipeline_help.md
var pipelineHelpTemplate string

// pipelineDocLimits are the capacity values the model reads. They come from
// the constants the runtime enforces, so the description cannot drift.
type pipelineDocLimits struct {
	Steps, Invocations                           int
	MinTimeoutMS, MaxTimeoutMS, DefaultTimeoutMS int64
	FlowNodes, FlowEstimate, FlowCalls           int
	FlowDefaultMS                                int64
	FlowItems, BrowserSteps                      int
	SequenceSteps, Inputs                        int
	SequenceSeconds, RecipeSeconds               int
	ContentKiB, OutputKiB                        int
	Topics                                       string
}

// pipelineStepContentLimit bounds one step's returned content.
const pipelineStepContentLimit = 8000

func currentPipelineDocLimits() pipelineDocLimits {
	return pipelineDocLimits{
		Steps: maxPipelineSteps, Invocations: maxPipelineInvocations,
		MinTimeoutMS: 1000, MaxTimeoutMS: maxPipelineTimeout.Milliseconds(), DefaultTimeoutMS: defaultPipelineTimeout.Milliseconds(),
		FlowNodes: maxDesktopFlowNodes, FlowEstimate: maxDesktopFlowEstimatedCalls, FlowCalls: maxDesktopFlowChildCalls,
		FlowDefaultMS: defaultDesktopFlowTimeout.Milliseconds(),
		FlowItems:     maxFlowItems, BrowserSteps: maxWorkflowSteps,
		SequenceSteps: desktopSequenceStepLimit, Inputs: desktopInputLimit,
		SequenceSeconds: int(desktopSequenceTimeout / time.Second), RecipeSeconds: int(desktopRecipeTimeout / time.Second),
		ContentKiB: (pipelineStepContentLimit + 1023) / 1024, OutputKiB: maxPipelineOutput / 1024,
		Topics: strings.Join(pipelineHelpTopicNames(), ", "),
	}
}

// renderPipelineDoc fills the {%...%} fields of a pipeline document. The
// delimiters differ from Go's default because the documents contain flow
// placeholders written as {{input}}.
func renderPipelineDoc(name, text string) string {
	tmpl := template.Must(template.New(name).Delims("{%", "%}").Option("missingkey=error").Parse(text))
	var b strings.Builder
	if err := tmpl.Execute(&b, currentPipelineDocLimits()); err != nil {
		panic(fmt.Sprintf("render %s: %v", name, err))
	}
	return strings.TrimSpace(b.String())
}

const pipelineHelpHeading = "## help: "

// pipelineHelpTopicNames lists the topics in document order.
func pipelineHelpTopicNames() []string {
	var names []string
	for _, line := range strings.Split(pipelineHelpTemplate, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), pipelineHelpHeading); ok {
			names = append(names, strings.TrimSpace(name))
		}
	}
	return names
}

// pipelineHelpTopics renders the help document once and splits it by topic.
var pipelineHelpTopics = func() map[string]string {
	rendered := renderPipelineDoc("tool_pipeline_help", pipelineHelpTemplate)
	topics := map[string]string{}
	var name string
	var body strings.Builder
	flush := func() {
		if name != "" {
			topics[name] = strings.TrimSpace(body.String())
		}
		body.Reset()
	}
	for _, line := range strings.Split(rendered, "\n") {
		if next, ok := strings.CutPrefix(strings.TrimSpace(line), pipelineHelpHeading); ok {
			flush()
			name = strings.TrimSpace(next)
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	flush()
	return topics
}()

// pipelineHelp answers a help request without running anything.
func pipelineHelp(p PipelineParams) (string, bool) {
	topic := strings.TrimSpace(p.Help)
	if p.Desktop != nil || p.Browser != nil || p.Flow != nil || len(p.Steps) > 0 || len(p.Return) > 0 {
		return "help cannot be combined with desktop, browser, flow, steps or return; ask for help in its own call. Nothing was run.", false
	}
	text, ok := pipelineHelpTopics[topic]
	if !ok {
		names := pipelineHelpTopicNames()
		slices.Sort(names)
		return fmt.Sprintf("unknown help topic %q; topics: %s. Nothing was run.", topic, strings.Join(names, ", ")), false
	}
	return "tool_pipeline help: " + topic + " (nothing was run)\n\n" + text, true
}
