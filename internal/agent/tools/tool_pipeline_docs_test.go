package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// The description states the limits the runtime enforces, renders every
// field, and stays small: details are served by help topics.
func TestPipelineDescriptionIsRenderedFromRuntimeLimits(t *testing.T) {
	t.Parallel()
	require.NotContains(t, pipelineDescription, "{%")
	for _, want := range []string{
		fmt.Sprintf("up to %d nodes", maxDesktopFlowNodes),
		fmt.Sprintf("1-%d semantic browser steps", maxWorkflowSteps),
		fmt.Sprintf("steps %d and %d invocations", maxPipelineSteps, maxPipelineInvocations),
		fmt.Sprintf("%d child calls", maxDesktopFlowChildCalls),
		fmt.Sprintf("sequence %d steps", desktopSequenceStepLimit),
		"Topics: desktop_flow, desktop_recipes, browser, browser_flow, steps, results, examples",
	} {
		require.Contains(t, pipelineDescription, want)
	}
	require.Less(t, len(pipelineDescription), 9000, "the always-sent description stays compact")

	// The schema enum and the help document name the same topics.
	info := NewToolPipeline(nil).Info()
	help, ok := info.Parameters["help"].(map[string]any)
	require.True(t, ok)
	var enum []string
	for _, v := range help["enum"].([]any) {
		enum = append(enum, v.(string))
	}
	require.Equal(t, pipelineHelpTopicNames(), enum)
	for _, topic := range enum {
		require.NotEmpty(t, pipelineHelpTopics[topic], topic)
		require.NotContains(t, pipelineHelpTopics[topic], "{%", topic)
	}
}

// The guidance the runtime enforces is stated where the model reads it; the
// behavior behind each statement is exercised by the desktop flow review
// and acceptance tests.
func TestPipelineDocsStateFlowEvidenceAndResumeRules(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"A checkpoint in another window never proves the input already took effect in its own window",
		`reports ` + "`" + `dispatch:"none"` + "`",
		"is never replayed automatically",
		"re-resolves every window",
		"question, permission or hook metadata is kept beside the flow ledger",
		"one input per turn or a screenshot per input",
		"never invent an identity",
	} {
		require.Contains(t, pipelineDescription, want)
	}
	for _, want := range []string{
		"never from error text",
		"also the first of several inputs",
		"a checkpoint in another window (`checkpoint_ref`) or on another control never reconciles or skips it",
	} {
		require.Contains(t, pipelineHelpTopics["desktop_flow"], want)
	}
	require.Contains(t, pipelineHelpTopics["desktop_flow"], "`child_atlas_outcome`")
	require.Contains(t, pipelineHelpTopics["examples"], "`resume:true` after the user's answer")
}

// help answers from the document and never dispatches a child call.
func TestPipelineHelpRunsNothing(t *testing.T) {
	t.Parallel()
	rec := &recordingInvoke{}
	resp := runPipelineInput(t, `{"help":"desktop_flow"}`, rec)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "closes_window")
	require.Contains(t, resp.Content, "nothing was run")
	resp = runPipelineInput(t, `{"help":"nope"}`, rec)
	require.True(t, resp.IsError)
	resp = runPipelineInput(t, `{"help":"steps","desktop":{"mode":"observe","window_id":"1"}}`, rec)
	require.True(t, resp.IsError)
	require.Zero(t, rec.count())
}

var exampleBlock = regexp.MustCompile("(?s)```json\n(.*?)\n```")

// examplePlaceholders are the identities documented examples leave to the
// caller's observation. Any other <...> value makes the test fail, so a
// documented example cannot rely on an unexplained placeholder.
var examplePlaceholders = map[string]string{
	"<result_element_id>":    "1001:result",
	"<calculator_window_id>": "1001",
	"<explorer_window_id>":   "1002",
}

func documentedExamples(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{"description": pipelineDescription, "browser_scoped": pipelineBrowserDescription}
	for topic, text := range pipelineHelpTopics {
		sources["help:"+topic] = text
	}
	examples := map[string]string{}
	for source, text := range sources {
		for i, match := range exampleBlock.FindAllStringSubmatch(text, -1) {
			body := match[1]
			// Windows desktop examples use a native fixture path on other
			// hosts; target syntax and every decoder assertion stay intact.
			if runtime.GOOS != "windows" {
				fixturePath, err := json.Marshal(filepath.Join(t.TempDir(), "hesap.txt"))
				require.NoError(t, err)
				body = strings.ReplaceAll(body, `"C:\\Users\\me\\Desktop\\Atlas-Test\\hesap.txt"`, string(fixturePath))
			}
			for placeholder, value := range examplePlaceholders {
				body = strings.ReplaceAll(body, placeholder, value)
			}
			require.NotRegexp(t, `"<[a-z_]+>"`, body, "%s example %d uses an undocumented placeholder", source, i)
			examples[fmt.Sprintf("%s#%d", source, i)] = body
		}
	}
	require.GreaterOrEqual(t, len(examples), 6)
	return examples
}

// Every documented example decodes with the real decoder (no string
// normalization needed), passes every preflight and reaches its first child
// call. An example the runtime would refuse cannot stay documented.
func TestEveryDocumentedExamplePassesTheRealDecoderAndPreflight(t *testing.T) {
	t.Parallel()
	for name, body := range documentedExamples(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var p PipelineParams
			require.NoError(t, json.Unmarshal([]byte(body), &p), body)
			require.Empty(t, p.normalized, "examples send objects, never strings holding JSON")
			calls := 0
			probe := func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				r := fantasy.NewTextErrorResponse("probe: stopped after the plan passed preflight")
				r.StopTurn = true
				return r, nil
			}
			ctx := context.WithValue(t.Context(), SessionIDContextKey, "docs-"+name)
			resp, err := NewToolPipeline(probe).Run(ctx, fantasy.ToolCall{ID: "doc", Name: ToolPipelineName, Input: body})
			require.NoError(t, err)
			require.Equal(t, 1, calls, "the example was refused before its first child call: %s", resp.Content)
		})
	}
}

// recordingPermissions wraps the real service and records every request, so
// a test can prove each pipeline child passed the ordinary permission check.
type recordingPermissions struct {
	permission.Service
	mu       sync.Mutex
	requests []permission.CreatePermissionRequest
	deny     func(permission.CreatePermissionRequest) bool
}

func (p *recordingPermissions) Request(ctx context.Context, opts permission.CreatePermissionRequest) (bool, error) {
	p.mu.Lock()
	p.requests = append(p.requests, opts)
	deny := p.deny != nil && p.deny(opts)
	p.mu.Unlock()
	if deny {
		return false, nil
	}
	return p.Service.Request(ctx, opts)
}

// longWorkflowFixture is the session 69361c0a task state before the long
// workflow: the folder window the task opened, a user document that must
// survive, and Calculator observed once.
func longWorkflowFixture(t *testing.T, permissions *recordingPermissions) (*simDesktop, *simTool, context.Context, string) {
	t.Helper()
	return longWorkflowFixtureIn(t, permissions, "")
}

// longWorkflowFixtureIn is longWorkflowFixture with the desktop folder at
// dir on the real file system when dir is set.
func longWorkflowFixtureIn(t *testing.T, permissions *recordingPermissions, dir string) (*simDesktop, *simTool, context.Context, string) {
	t.Helper()
	d := newSimDesktop()
	if dir != "" {
		d.desktopDir, d.onDisk = dir, true
	}
	userDoc := d.open("other", "Rapor.docx - Word", 7777, "WINWORD.EXE", "OpusApp")
	_ = userDoc
	folder := d.open("explorer", "Atlas-Test", 4100, "explorer.exe", "CabinetWClass")
	folder.folder = d.desktopDir + `\Atlas-Test`
	d.foreground = userDoc.info.ID
	if permissions == nil {
		permissions = &recordingPermissions{Service: permission.NewPermissionService(t.TempDir(), true, nil)}
	}
	sim := newSimTool(t, d, permissions)
	ctx := simContext(t)
	// The folder window was opened by an earlier task input.
	state := desktopTransitionsFor(desktopFlowSession(ctx))
	state.mu.Lock()
	state.taskOpened[folder.info.ID] = folder.info
	state.mu.Unlock()

	// Call 1: observe the calculator once and read its result element.
	resp := sim.pipeline(t, ctx, `{"desktop":{"mode":"prepare","application":"Hesap Makinesi","observation":"semantic"}}`)
	require.False(t, resp.IsError, resp.Content)
	var observed struct {
		Elements []desktopElement `json:"elements"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &observed), resp.Content)
	result := ""
	for _, e := range observed.Elements {
		if e.AutomationID == "CalculatorResults" {
			result = e.ID
		}
	}
	require.NotEmpty(t, result, "the observation exposes the result element")
	return d, sim, ctx, result
}

func documentedLongWorkflow(t *testing.T, resultElement string) string {
	t.Helper()
	blocks := exampleBlock.FindAllStringSubmatch(pipelineHelpTopics["examples"], -1)
	require.Len(t, blocks, 1)
	return strings.ReplaceAll(blocks[0][1], "<result_element_id>", resultElement)
}

type flowReport struct {
	Flow struct {
		Nodes  []map[string]string `json:"nodes"`
		Steps  []map[string]any    `json:"steps"`
		Calls  int                 `json:"calls"`
		Resume string              `json:"resume"`
	} `json:"flow"`
}

func flowReportOf(t *testing.T, content string) flowReport {
	t.Helper()
	start := strings.LastIndex(content, "\n{\"flow\"")
	require.GreaterOrEqual(t, start, 0, content)
	var report flowReport
	require.NoError(t, json.Unmarshal([]byte(content[start+1:]), &report), content)
	return report
}

func nodeStates(report flowReport) map[string]string {
	out := map[string]string{}
	for _, n := range report.Flow.Nodes {
		out[n["id"]] = n["state"]
	}
	return out
}

// The documented long workflow, executed by the real decoder, flow executor
// and computer tool against the simulated desktop: three calculations with
// three checkpoints, writing and saving through the owned dialog with a
// verified return to Notepad, a rename whose hidden extension is read from
// the shell view, reopening with a content check, and closing exactly the
// three windows the task opened. The user's document window is untouched.
func TestDocumentedLongDesktopWorkflowRunsEndToEnd(t *testing.T) {
	t.Parallel()
	permissions := &recordingPermissions{Service: permission.NewPermissionService(t.TempDir(), true, nil)}
	d, sim, ctx, result := longWorkflowFixture(t, permissions)
	calc := d.window(d.foreground).info.ID
	before := len(sim.calls)
	requestsBefore := len(permissions.requests)

	started := time.Now()
	resp := sim.pipeline(t, ctx, documentedLongWorkflow(t, result))
	elapsed := time.Since(started)
	require.False(t, resp.IsError, resp.Content)
	require.NotEqual(t, "image", resp.Type, "no screenshot was needed")
	report := flowReportOf(t, resp.Content)
	for id, state := range nodeStates(report) {
		require.Contains(t, []string{flowNodeBound, flowNodeVerified}, state, id)
	}
	require.Len(t, report.Flow.Nodes, 16)

	folder := d.desktopDir + `\Atlas-Test`
	require.Equal(t, "1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000", d.files[folder+`\sonuc.txt`])
	require.NotContains(t, d.files, folder+`\hesap.txt`)
	var titles []string
	for _, w := range d.windows {
		titles = append(titles, w.info.Name)
	}
	require.ElementsMatch(t, []string{"Rapor.docx - Word", "Atlas-Test"}, titles, "only the task's Calculator and Notepad windows were closed")
	for _, input := range d.inputs {
		require.NotContains(t, input, "focus:1001", "the user document window was never focused")
	}
	require.Nil(t, d.window(calc))

	children := sim.calls[before:]
	inputs := 0
	for _, c := range children {
		if desktopMutation(c.Action) && c.Action != "focus" {
			inputs++
		}
	}
	require.GreaterOrEqual(t, len(children), 20, "at least 20 inner steps")
	require.Equal(t, len(children), len(permissions.requests)-requestsBefore, "every child passed the permission check")
	t.Logf("long workflow: 1 top-level call, %d nodes, %d child calls (%d input actions), 0 images, %s", len(report.Flow.Nodes), len(children), inputs, elapsed.Round(time.Millisecond))
}

// A wrong intermediate result stops the flow at its checkpoint: no later
// mutation runs, nothing is saved, and the ledger separates verified,
// uncertain and not-run nodes.
func TestLongWorkflowStopsDependentMutationsAfterFailedCheckpoint(t *testing.T) {
	t.Parallel()
	d, sim, ctx, result := longWorkflowFixture(t, nil)
	plan := strings.Replace(documentedLongWorkflow(t, result), "Ekran değeri 8.638", "Ekran değeri 8.639", 1)
	plan = strings.Replace(plan, `"wait_ms":3000},"next":"add"`, `"wait_ms":300},"next":"add"`, 1)
	resp := sim.pipeline(t, ctx, plan)
	require.True(t, resp.IsError, resp.Content)
	states := nodeStates(flowReportOf(t, resp.Content))
	require.Equal(t, flowNodeVerified, states["mul"])
	require.Equal(t, flowNodeUncertain, states["div"], "its input was sent and its result is wrong")
	for _, id := range []string{"add", "editor", "write", "name", "rename", "close_calc"} {
		require.Equal(t, flowNodeNotRun, states[id], id)
	}
	require.Empty(t, d.files)
	require.Equal(t, 1, d.launches, "Notepad was never launched")
	require.Contains(t, flowReportOf(t, resp.Content).Flow.Resume, "may have taken effect")
}

// A permission denial in the middle stops the flow before the denied input
// and before every later input.
func TestLongWorkflowPermissionDenialStopsBeforeInput(t *testing.T) {
	t.Parallel()
	permissions := &recordingPermissions{Service: permission.NewPermissionService(t.TempDir(), true, nil)}
	d, sim, ctx, result := longWorkflowFixture(t, permissions)
	permissions.mu.Lock()
	permissions.deny = func(r permission.CreatePermissionRequest) bool { return r.Action == "hotkey" }
	permissions.mu.Unlock()
	resp := sim.pipeline(t, ctx, documentedLongWorkflow(t, result))
	require.True(t, resp.IsError)
	states := nodeStates(flowReportOf(t, resp.Content))
	require.Equal(t, flowNodeVerified, states["add"])
	// The computer tool reports the denied shortcut as dispatch:none and the
	// typed text was sent before it, so the operation as a whole is uncertain
	// and is never replayed.
	require.Equal(t, flowNodeUncertain, states["write"])
	require.Equal(t, flowNodeNotRun, states["dialog"])
	require.Empty(t, d.files)
	for _, input := range d.inputs {
		require.NotEqual(t, "hotkey:ctrl+s", input, "the denied shortcut was not sent")
	}
}

// Cancellation in the middle sends no further input.
func TestLongWorkflowCancellationSendsNoFurtherInput(t *testing.T) {
	t.Parallel()
	d, sim, ctx, result := longWorkflowFixture(t, nil)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	invoke := sim.invoke
	resp, err := NewToolPipeline(func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		r, err := invoke(ctx, call)
		if strings.Contains(call.Input, `"text":"/8="`) {
			cancel()
		}
		return r, err
	}).Run(ctx, fantasy.ToolCall{ID: "pipeline", Name: ToolPipelineName, Input: documentedLongWorkflow(t, result)})
	require.True(t, resp.IsError || err != nil)
	d.mu.Lock()
	defer d.mu.Unlock()
	last := d.inputs[len(d.inputs)-1]
	require.Equal(t, "type:/8=", last, "no input after the cancel")
	require.False(t, slices.Contains(d.inputs, "type:+362="))
}

// A close node refuses a window that existed before the task, before focus
// or input, unless the user explicitly asked for it.
func TestFlowCloseNeverClosesUserWindowWithoutPermission(t *testing.T) {
	t.Parallel()
	d, sim, ctx, _ := longWorkflowFixture(t, nil)
	plan := `{"desktop":{"mode":"flow","flow":{"nodes":[{"id":"doc","kind":"resolve","title":"Rapor.docx - Word","next":"close"},{"id":"close","kind":"close","window_ref":"doc"}]}}}`
	before := len(d.inputs)
	resp := sim.pipeline(t, ctx, plan)
	require.True(t, resp.IsError)
	require.True(t, strings.HasPrefix(resp.Content, "not_task_owned:"), resp.Content)
	require.Equal(t, before, len(d.inputs), "no focus and no key reached any window")
	require.NotNil(t, d.window("1001"))

	resp = sim.pipeline(t, ctx, strings.Replace(plan, `"window_ref":"doc"}`, `"window_ref":"doc","allow_unowned":true}`, 1))
	require.False(t, resp.IsError, resp.Content)
	require.Nil(t, d.window("1001"), "an explicit request may close it")
}

// A durable flow interrupted after a verified checkpoint resumes without
// repeating completed input and re-resolves its window instead of trusting
// the handle it used before. When the application was restarted, the new
// handle is used and the old one is never addressed.
func TestDurableFlowResumeRejectsStaleHandles(t *testing.T) {
	t.Parallel()
	d, sim, ctx, _ := longWorkflowFixture(t, nil)
	ctx = context.WithValue(ctx, desktopFlowStoreKey{}, t.TempDir())
	plan := `{"desktop":{"mode":"flow","flow":{"run_id":"calc-1","nodes":[
 {"id":"calc","kind":"resolve","application":"Hesap Makinesi","next":"mul"},
 {"id":"mul","kind":"operation","window_ref":"calc","focus":true,"input":{"action":"type","text":"1234*56="},"checkpoint":{"name":"Ekran değeri 69.104","condition":"visible","wait_ms":2000},"next":"div"},
 {"id":"div","kind":"operation","window_ref":"calc","focus":true,"input":{"action":"type","text":"/8="},"checkpoint":{"name":"Ekran değeri 8.638","condition":"visible","wait_ms":2000}}]}}}`
	oldCalc := ""
	for _, w := range d.windows {
		if w.kind == "calculator" {
			oldCalc = w.info.ID
		}
	}
	interrupted, stop := context.WithCancel(ctx)
	invoke := sim.invoke
	resp, err := NewToolPipeline(func(c context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		r, err := invoke(c, call)
		if strings.Contains(call.Input, `"name":"Ekran değeri 69.104"`) && !r.IsError {
			stop()
		}
		return r, err
	}).Run(interrupted, fantasy.ToolCall{ID: "first", Name: ToolPipelineName, Input: plan})
	require.True(t, err != nil || resp.IsError)

	// The application restarted with a new handle and the same state.
	d.mu.Lock()
	old := d.window(oldCalc)
	restarted := d.open("calculator", "Hesap Makinesi", 901, "ApplicationFrameHost.exe", "ApplicationFrameWindow")
	restarted.acc, restarted.shown = old.acc, old.shown
	d.close(old)
	mark := len(d.inputs)
	d.mu.Unlock()

	resp = sim.pipeline(t, ctx, strings.Replace(plan, `"run_id":"calc-1"`, `"run_id":"calc-1","resume":true`, 1))
	require.False(t, resp.IsError, resp.Content)
	d.mu.Lock()
	defer d.mu.Unlock()
	after := d.inputs[mark:]
	require.NotContains(t, after, "type:1234*56=", "the verified operation was not repeated")
	require.Contains(t, after, "type:/8=")
	for _, input := range after {
		require.NotEqual(t, "focus:"+oldCalc, input, "the stale handle was never used")
	}
	require.Contains(t, after, "focus:"+restarted.info.ID)
}

// A question or permission stop on a read-only node keeps progress; the
// resumed run continues there without repeating earlier mutations.
func TestDurableFlowContinuesAfterQuestionWithoutRepeatingMutations(t *testing.T) {
	t.Parallel()
	d, sim, ctx, _ := longWorkflowFixture(t, nil)
	ctx = context.WithValue(ctx, desktopFlowStoreKey{}, t.TempDir())
	plan := `{"desktop":{"mode":"flow","flow":{"run_id":"ask-1","nodes":[
 {"id":"calc","kind":"resolve","application":"Hesap Makinesi","next":"mul"},
 {"id":"mul","kind":"operation","window_ref":"calc","focus":true,"input":{"action":"type","text":"1234*56="},"checkpoint":{"name":"Ekran değeri 69.104","condition":"visible","wait_ms":2000},"next":"check"},
 {"id":"check","kind":"verify","window_ref":"calc","checkpoint":{"name":"Ekran değeri 69.104","condition":"enabled"},"next":"add"},
 {"id":"add","kind":"operation","window_ref":"calc","focus":true,"input":{"action":"type","text":"+1="},"checkpoint":{"name":"Ekran değeri 69.105","condition":"visible","wait_ms":2000}}]}}}`
	invoke := sim.invoke
	asked := false
	resp, err := NewToolPipeline(func(c context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if strings.Contains(call.Input, `"condition":"enabled"`) && !asked {
			asked = true
			r := fantasy.NewTextErrorResponse("question: the user must confirm before continuing")
			r.StopTurn = true
			return r, nil
		}
		return invoke(c, call)
	}).Run(ctx, fantasy.ToolCall{ID: "first", Name: ToolPipelineName, Input: plan})
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	require.Equal(t, flowNodeBlocked, nodeStates(flowReportOf(t, resp.Content))["check"])

	resp = sim.pipeline(t, ctx, strings.Replace(plan, `"run_id":"ask-1"`, `"run_id":"ask-1","resume":true`, 1))
	require.False(t, resp.IsError, resp.Content)
	d.mu.Lock()
	defer d.mu.Unlock()
	typed := 0
	for _, input := range d.inputs {
		if input == "type:1234*56=" {
			typed++
		}
	}
	require.Equal(t, 1, typed, "the multiplication was sent once across both runs")
	require.Contains(t, d.inputs, "type:+1=")
}

// A generic program of 160 steps whose results exceed the output budget
// keeps every step's identity, status, outcome and dispatch.
func TestPipelineOutputLimitKeepsEveryStepState(t *testing.T) {
	t.Parallel()
	steps := make([]PipelineStep, maxPipelineSteps)
	for i := range steps {
		steps[i] = PipelineStep{ID: fmt.Sprintf("s%03d", i), Tool: ComputerToolName, Arguments: map[string]any{"action": "windows"}}
	}
	data, err := json.Marshal(PipelineParams{Steps: steps})
	require.NoError(t, err)
	big := strings.Repeat("x", 7900)
	rec := &recordingInvoke{reply: func(fantasy.ToolCall) fantasy.ToolResponse { return fantasy.NewTextResponse(big) }}
	resp := runPipelineInput(t, string(data), rec)
	require.False(t, resp.IsError)
	require.LessOrEqual(t, len(resp.Content), maxPipelineOutput)
	var results []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &results))
	require.Len(t, results, maxPipelineSteps)
	for i, r := range results {
		require.Equal(t, fmt.Sprintf("s%03d", i), r.ID)
		require.Equal(t, StepSucceeded, r.Status)
		require.Equal(t, OutcomeApplied, r.Outcome)
	}
}

// A computer step whose arguments the computer tool would refuse fails at
// preflight, before the earlier steps run.
func TestPipelinePreflightsComputerStepsBeforeAnyInput(t *testing.T) {
	t.Parallel()
	for name, bad := range map[string]string{
		"unknown action": `{"action":"tap","automation":{"window_id":"11"}}`,
		"recipe mode":    `{"action":"sequence"}`,
		"unknown key":    `{"action":"key","key":"hyper","automation":{"window_id":"11"}}`,
		"no modifiers":   `{"action":"hotkey","key":"s","automation":{"window_id":"11"}}`,
		"bad window":     `{"action":"type","text":"x","automation":{"window_id":"Not Defteri"}}`,
		"unknown field":  `{"action":"type","text":"x","colour":"red","automation":{"window_id":"11"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingInvoke{}
			input := `{"steps":[{"id":"first","tool":"computer","arguments":{"action":"type","text":"a","automation":{"window_id":"11"}}},{"id":"late","tool":"computer","arguments":` + bad + `}]}`
			resp := runPipelineInput(t, input, rec)
			require.True(t, resp.IsError, resp.Content)
			require.Contains(t, resp.Content, "step late")
			require.Contains(t, resp.Content, "No step of this pipeline was run")
			require.Zero(t, rec.count())
		})
	}
	// A bound window that is not a native handle stops before that input.
	rec := &recordingInvoke{reply: func(call fantasy.ToolCall) fantasy.ToolResponse {
		return fantasy.NewTextResponse(`{"title":"Not Defteri"}`)
	}}
	resp := runPipelineInput(t, `{"steps":[{"id":"read","tool":"computer","arguments":{"action":"windows"}},{"id":"type","tool":"computer","arguments":{"action":"type","text":"x"},"bindings":{"automation.window_id":"read#/title"}}]}`, rec)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "not a native window handle")
	require.Equal(t, 1, rec.count())
}
