package tools

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

//go:embed tool_pipeline.md
var pipelineTemplate string

// pipelineDescription is the model-visible description, rendered with the
// limits the runtime enforces. Details live in help topics.
var pipelineDescription = renderPipelineDoc("tool_pipeline", pipelineTemplate)

type PipelineStep struct {
	Bindings        map[string]string  `json:"bindings,omitempty" description:"Copy a scalar JSON result into an argument path. Example advanced.tab_id: popup#/tab_id, advanced.viewport_id: shot#/viewport_id, ref: found#/matches/0/ref, automation.window_id: launch#/input_settle/new_window_ids/0 (a single window opened by that step). Sources must be earlier steps; items cannot be combined."`
	RequirePassed   bool               `json:"require_passed,omitempty" description:"Require passed:true from computer assert or browser assert/wait_for/download_wait/popup_wait/dialog_wait before continuing."`
	ObservationFrom string             `json:"observation_from,omitempty" description:"Earlier computer observe step supplying snapshot_id for this computer call."`
	ID              string             `json:"id"`
	Tool            string             `json:"tool"`
	Arguments       map[string]any     `json:"arguments"`
	Items           []string           `json:"items,omitempty"`
	IfSuccess       string             `json:"if_success,omitempty"`
	IfFailed        string             `json:"if_failed,omitempty" description:"Run only when that earlier step failed: a recovery or fallback branch. Its failure no longer stops the pipeline before this branch."`
	OnError         string             `json:"on_error,omitempty" description:"stop (default) or continue. continue is for independent steps only; steps depending on this one should use if_success."`
	When            *PipelineCondition `json:"when,omitempty" description:"Run only if an earlier step's JSON result matches, e.g. {\"source\":\"found#/count\",\"equals\":\"0\"}. Unmet or unreadable: skipped with the reason."`
	RetryOn         []string           `json:"retry_on,omitempty" description:"Browser steps with a semantic target (role/name/label, no ref or selector) only: error codes that allow ONE retry after a fresh observe, and only when nothing was sent: target_not_found, target_resolution_failed, condition_timeout, stale_ref."`
}

// PipelineCondition compares one scalar of an earlier step's result. Give
// exactly one of equals, not_equals or exists.
type PipelineCondition struct {
	Source    string  `json:"source" description:"Earlier step result field: step#/json/pointer, e.g. found#/count or width#/accepted_value."`
	Equals    *string `json:"equals,omitempty" description:"Run when the field's text equals this."`
	NotEquals *string `json:"not_equals,omitempty" description:"Run when the field's text differs from this."`
	Exists    *bool   `json:"exists,omitempty" description:"true: run when the field exists; false: run when it does not."`
}

// pipelineRetryCodes are failures that were refused before any input and
// can change with a fresh observation of a semantic target.
var pipelineRetryCodes = map[string]bool{"target_not_found": true, "target_resolution_failed": true, "condition_timeout": true, "stale_ref": true}

type (
	PipelineParams struct {
		Browser   *BrowserWorkflowParams `json:"browser,omitempty" description:"Sequential browser workflow with semantic targets, ownership guards, waits and optional DOM postconditions. Each step uses the ordinary hooked browser tool. Stops at the first failure unless a step says on_error=continue or a later step handles it with if_failed; no mutation replay."`
		Flow      *BrowserFlowParams     `json:"flow,omitempty" description:"Reusable parameterized browser flow: a template of workflow steps with {{input}} placeholders run once per item, results kept per item, and rerun of only failed or uncertain items."`
		Desktop   *DesktopWorkflowParams `json:"desktop,omitempty" description:"Bounded desktop recipe instead of steps: observe, prepare, rename, sequence, adaptive, transition or flow. Flow composes up to 64 prepare/resolve/branch/operation/verify/rename/close nodes, 16 inputs per operation, fresh window bindings and optional session-scoped durable resume. Rename verifies Explorer selection/editor/committed name; close focuses and proves absence."`
		Steps     []PipelineStep         `json:"steps,omitempty"`
		Help      string                 `json:"help,omitempty" enum:"desktop_flow,desktop_recipes,browser,browser_flow,steps,results,examples" description:"Return the detailed rules and executable examples for one topic instead of running anything. Use it alone."`
		Return    []string               `json:"return,omitempty"`
		TimeoutMS int                    `json:"timeout_ms,omitempty" description:"Overall bound for steps/browser/flow runs, 1000-900000 ms (default 300000). Cancellation and this deadline reach every child call."`
		// normalized lists object fields that arrived string-encoded.
		normalized []PipelineNormalization
	}
	PipelineResult struct {
		DesktopState *DesktopEvidence `json:"desktop_state,omitempty"`
		ID           string           `json:"id"`
		Iteration    int              `json:"iteration"`
		Status       string           `json:"status"`
		// Outcome is the shared lifecycle state of this step: planned,
		// dispatched, applied, verified, unknown, blocked, canceled or
		// failed. Only verified is a checked result; applied means the
		// step ran.
		Outcome       string `json:"outcome"`
		Reason        string `json:"reason,omitempty"`
		IsError       bool   `json:"is_error"`
		ErrorCode     string `json:"error_code,omitempty"`
		Dispatch      string `json:"dispatch,omitempty"`
		DurationMS    int64  `json:"duration_ms"`
		Attempts      int    `json:"attempts,omitempty"`
		Content       string `json:"content"`
		Metadata      string `json:"metadata,omitempty"`
		MediaType     string `json:"media_type,omitempty"`
		ImageAttached bool   `json:"image_attached,omitempty"`
		ImageOmitted  bool   `json:"image_omitted,omitempty"`
		// Resume is set on the step that stopped the pipeline: which steps
		// already took effect and where a follow-up call continues.
		Resume string `json:"resume,omitempty"`
	}
)

// Pipeline step statuses. Uncertain means the step failed after input may
// have reached the page, so its effect must be observed before any retry.
const (
	StepSucceeded = "succeeded"
	StepFailed    = "failed"
	StepSkipped   = "skipped"
	StepUncertain = "uncertain"
	StepStopped   = "stopped"
)

// Pipeline limits: enough for a complete multi-part UI build in one call
// (64 logical browser steps with verification), bounded so a runaway plan
// cannot hold the session.
const (
	maxPipelineSteps       = 160
	maxPipelineInvocations = 192
	defaultPipelineTimeout = 5 * time.Minute
	maxPipelineTimeout     = 15 * time.Minute
)

func NewToolPipeline(invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error), stores ...*engineering.Store) fantasy.AgentTool {
	return fantasy.ShapeAgentToolSchema(newToolPipeline(invoke, stores...), withoutNestedDesktopBatch)
}

// withoutNestedDesktopBatch removes computer batches from desktop recipe
// inputs. validateDesktopChild already rejects them, so advertising the
// batch shape in every input position only cost schema size and invited a
// call the runtime refuses.
func withoutNestedDesktopBatch(root *fantasy.Schema) {
	desktop := root.Properties["desktop"]
	if desktop == nil {
		return
	}
	schema.Walk(desktop, func(_ string, node *fantasy.Schema) {
		action := node.Properties["action"]
		if node.Properties["batch"] == nil || action == nil || !slices.Contains(action.Enum, any("batch")) {
			return
		}
		delete(node.Properties, "batch")
		action.Enum = slices.DeleteFunc(slices.Clone(action.Enum), func(value any) bool { return value == "batch" })
		if action.Description != "" {
			action.Description = "One known desktop action for this recipe input; nested batch is not allowed here. " + action.Description
		}
	})
}

func pipelineTimeout(ms int) (time.Duration, error) {
	if ms == 0 {
		return defaultPipelineTimeout, nil
	}
	if ms < 1000 || time.Duration(ms)*time.Millisecond > maxPipelineTimeout {
		return 0, fmt.Errorf("timeout_ms must be 1000-%d", maxPipelineTimeout.Milliseconds())
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func newToolPipeline(invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error), stores ...*engineering.Store) fantasy.AgentTool {
	return fantasy.NewAgentTool("tool_pipeline", pipelineDescription, func(ctx context.Context, p PipelineParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var effects pipelineEffects
		observedInvoke := func(ctx context.Context, child fantasy.ToolCall) (fantasy.ToolResponse, error) {
			response, err := invoke(ctx, child)
			effects.observe(child, response, err)
			return response, err
		}
		resp, err := runToolPipeline(ctx, p, call, observedInvoke, stores...)
		resp = effects.response(resp)
		return annotateNormalization(resp, p.normalized), err
	})
}

func runToolPipeline(ctx context.Context, p PipelineParams, call fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error), stores ...*engineering.Store) (fantasy.ToolResponse, error) {
	if p.Help != "" {
		text, ok := pipelineHelp(p)
		if !ok {
			return fantasy.NewTextErrorResponse(text), nil
		}
		return fantasy.NewTextResponse(text), nil
	}
	timeout, err := pipelineTimeout(p.TimeoutMS)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if p.Flow != nil {
		if p.Browser != nil || p.Desktop != nil || len(p.Steps) > 0 {
			return fantasy.NewTextErrorResponse("flow cannot be combined with browser, desktop or steps"), nil
		}
		dir := ""
		if len(stores) > 0 && stores[0] != nil {
			dir = stores[0].Dir()
		}
		return runBrowserFlow(ctx, *p.Flow, dir, timeout, call, invoke)
	}
	if p.Browser != nil {
		if p.Desktop != nil || len(p.Steps) > 0 {
			return fantasy.NewTextErrorResponse("browser workflow cannot be combined with desktop or steps"), nil
		}
		p.Steps, err = compileBrowserWorkflow(*p.Browser)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
	}
	if len(stores) > 0 && stores[0] != nil {
		ctx = context.WithValue(ctx, desktopFlowStoreKey{}, stores[0].Dir())
	}
	if p.Desktop != nil {
		if len(p.Steps) > 0 || len(p.Return) > 0 {
			return fantasy.NewTextErrorResponse("desktop recipes cannot be combined with steps or return"), nil
		}
		if p.TimeoutMS != 0 {
			// Only a flow uses the overall pipeline bound; the other recipes
			// keep their own short, operation-specific limits.
			ctx = context.WithValue(ctx, desktopFlowTimeoutKey{}, timeout)
		}
		return runDesktopWorkflow(ctx, *p.Desktop, call, invoke)
	}
	if resp, invalid := validatePipeline(ctx, p.Steps, p.Return); invalid {
		return resp, nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	run, err := executePipeline(ctx, p.Steps, p.Return, call, invoke)
	if err != nil {
		run.invalid = err.Error()
	}
	return pipelineResponse(run, p.Return)
}

// validatePipeline checks the whole plan before the first call runs, so
// a malformed later step cannot strand an earlier mutation.
func validatePipeline(ctx context.Context, steps []PipelineStep, ret []string) (fantasy.ToolResponse, bool) {
	if msg := validatePipelinePlan(ctx, steps, ret); msg != "" {
		return fantasy.NewTextErrorResponse(msg), true
	}
	for _, s := range steps {
		if resp, blocked := desktopScopeViolation(ctx, s.Tool); blocked {
			return resp, true
		}
	}
	return fantasy.ToolResponse{}, false
}

func validatePipelinePlan(ctx context.Context, steps []PipelineStep, ret []string) string {
	if len(steps) < 1 || len(steps) > maxPipelineSteps {
		return fmt.Sprintf("pipeline requires 1-%d steps", maxPipelineSteps)
	}
	seen := map[string]bool{}
	observations := map[string]bool{}
	count := 0
	for i, s := range steps {
		if msg := validatePipelineStep(s, seen, observations); msg != "" {
			return pipelineStepError(i, s.ID, msg)
		}
		seen[s.ID] = true
		observations[s.ID] = s.Tool == ComputerToolName && s.Arguments["action"] == "observe" && len(s.Items) == 0
		count += max(1, len(s.Items))
	}
	if count > maxPipelineInvocations {
		return fmt.Sprintf("pipeline is limited to %d invocations", maxPipelineInvocations)
	}
	for _, id := range ret {
		if !seen[id] {
			return "unknown return step"
		}
	}
	return ""
}

// pipelineStepError names the refused step by index and id unless the
// message already does, so the model fixes that step instead of guessing.
// A structured contract keeps its own field path and stays unchanged.
func pipelineStepError(i int, id, msg string) string {
	if strings.HasPrefix(msg, "step ") || strings.Contains(msg, "Tool contract: ") {
		return msg
	}
	encoded, _ := json.Marshal(id)
	return fmt.Sprintf("steps[%d] (id %s): %s. No step of this pipeline was run", i, encoded, strings.TrimSuffix(msg, "; no step was sent"))
}

// validatePipelineStep checks one step against the steps before it.
func validatePipelineStep(s PipelineStep, seen, observations map[string]bool) string {
	for path, source := range s.Bindings {
		id, _, ok := strings.Cut(source, "#/")
		if !ok || !seen[id] || len(s.Items) > 0 || !validPipelineBindingPath(path) {
			return "invalid binding: use an earlier step#/field and a supported argument path"
		}
	}
	if s.RequirePassed && !pipelineAssertionStep(s) {
		return "require_passed requires a computer assert or browser assert/wait/download/popup/dialog condition"
	}
	if s.ObservationFrom != "" && (s.Tool != ComputerToolName || !observations[s.ObservationFrom] || len(s.Items) > 0) {
		return "observation_from requires an earlier single computer observe step"
	}
	if encodedID, _ := json.Marshal(s.ID); len(encodedID) > 66 {
		return "step id exceeds the 64-byte JSON identity budget; no step was sent"
	}
	if s.ID == "" || seen[s.ID] || s.Tool == "" || s.Tool == "tool_pipeline" || s.Tool == "agent_jobs" || s.Tool == "task_board" || s.Tool == "goal" {
		return "invalid step identity or recursive/control tool"
	}
	if s.IfSuccess != "" && !seen[s.IfSuccess] {
		return "if_success must reference an earlier step"
	}
	if s.IfFailed != "" && (!seen[s.IfFailed] || s.IfSuccess != "") {
		return "if_failed must reference an earlier step and cannot be combined with if_success"
	}
	if s.OnError != "" && s.OnError != "stop" && s.OnError != "continue" {
		return "on_error must be stop or continue"
	}
	if msg := validatePipelineCondition(s, seen); msg != "" {
		return msg
	}
	if msg := validatePipelineRetry(s); msg != "" {
		return msg
	}
	if msg := browserStepPreflight(s); msg != "" {
		return msg
	}
	if msg := computerStepPreflight(s); msg != "" {
		return msg
	}
	return ""
}

// browserStepPreflight applies the browser argument contract to a step
// before the pipeline starts, so a step that cannot run as written is
// reported while nothing has been sent yet.
func browserStepPreflight(s PipelineStep) string {
	if s.Tool != BrowserToolName {
		return ""
	}
	arguments := s.Arguments
	if s.Bindings["advanced.value"] != "" || s.Bindings["text"] != "" || s.Bindings["advanced.expected"] != "" {
		// A bound value arrives at run time; check the rest of the step now.
		arguments = map[string]any{}
		for k, v := range s.Arguments {
			arguments[k] = v
		}
		advanced := map[string]any{}
		if existing, ok := s.Arguments["advanced"].(map[string]any); ok {
			for k, v := range existing {
				advanced[k] = v
			}
		}
		for path := range s.Bindings {
			switch path {
			case "advanced.value", "advanced.expected":
				advanced[strings.TrimPrefix(path, "advanced.")] = "bound"
			case "text":
				arguments["text"] = "bound"
			}
		}
		arguments["advanced"] = advanced
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return ""
	}
	var params BrowserParams
	if err := json.Unmarshal(encoded, &params); err != nil {
		// The same decoding contract the step meets at run time.
		return fmt.Sprintf("step %s: %s. No step of this pipeline was run", s.ID, err.Error())
	}
	action := strings.ToLower(strings.TrimSpace(params.Action))
	refBound := s.Bindings["ref"] != "" || s.Bindings["selector"] != ""
	if err := browserArgumentPreflight(action, params, refBound); err != nil {
		return fmt.Sprintf("step %s: %s. No step of this pipeline was run", s.ID, err.Error())
	}
	return ""
}

// computerStepPreflight decodes a computer step with the computer tool's own
// argument contract before the pipeline starts, so an unknown action, an
// unknown field, a misplaced recipe mode or an invalid key fails while nothing
// has been sent instead of after earlier steps already took effect. Values
// supplied by bindings or items are checked again when they arrive.
func computerStepPreflight(s PipelineStep) string {
	if s.Tool != ComputerToolName {
		return ""
	}
	arguments := map[string]any{}
	for k, v := range s.Arguments {
		arguments[k] = v
	}
	if len(s.Bindings) > 0 {
		automation := map[string]any{}
		if existing, ok := s.Arguments["automation"].(map[string]any); ok {
			for k, v := range existing {
				automation[k] = v
			}
		}
		for path := range s.Bindings {
			// Stand-ins of the right type; the real values are bound at run
			// time and validated again by the child call.
			switch path {
			case "automation.window_id":
				automation["window_id"] = "1"
			case "automation.element_id":
				automation["element_id"] = "bound"
			case "automation.expected":
				automation["expected"] = "bound"
			case "text":
				arguments["text"] = "bound"
			}
		}
		if len(automation) > 0 {
			arguments["automation"] = automation
		}
	}
	if len(s.Items) > 0 {
		arguments = substitutePipeline(arguments, "item").(map[string]any)
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return ""
	}
	var params ComputerParams
	if err := json.Unmarshal(encoded, &params); err != nil {
		return fmt.Sprintf("step %s: %s. No step of this pipeline was run", s.ID, err.Error())
	}
	action := strings.ToLower(strings.TrimSpace(params.Action))
	if !slices.Contains(computerActions, action) {
		return fmt.Sprintf("step %s: %s. No step of this pipeline was run", s.ID, unknownComputerAction(params.Action).Content)
	}
	if params.File != nil && action != "file_check" {
		return fmt.Sprintf("step %s: file parameters require action file_check. No step of this pipeline was run", s.ID)
	}
	if action == "file_check" {
		if err := validateDesktopFileCheck(params.File); err != nil {
			return fmt.Sprintf("step %s: %s. No step of this pipeline was run", s.ID, err.Error())
		}
	}
	if params.Automation.WindowID != "" {
		if err := computer.ValidateAutomationRequest(params.Automation); err != nil {
			return fmt.Sprintf("step %s: %s. No step of this pipeline was run", s.ID, err.Error())
		}
	}
	// A scoped input is checked with the recipe input contract (keys,
	// modifiers, coordinates, text and selectors). Unscoped input and
	// snapshot references keep their own run-time checks.
	scoped := params.Automation.WindowID != "" && params.Element == 0 && params.SnapshotID == "" && s.ObservationFrom == ""
	switch action {
	case "key", "hotkey", "type", "invoke", "set_value", "click", "double_click", "right_click", "drag":
		if !scoped {
			break
		}
		probe := params
		probe.Action = action
		if err := validateDesktopInput(probe); err != nil {
			return fmt.Sprintf("step %s: %s. No step of this pipeline was run", s.ID, err.Error())
		}
	}
	return ""
}

type pipelineRun struct {
	results        []PipelineResult
	imageData      []byte
	imageMediaType string
	imageIndex     int
	imageProtected bool
	imageProofHash [32]byte
	imageMasks     int
	failed         bool
	resumeStep     string
	stopTurn       *fantasy.ToolResponse
	invalid        string
}

// pipelineStepUncertain reports a failed step whose effect is unknown,
// using both the tool's content and its metadata.
func pipelineStepUncertain(content string, metadata ...string) bool {
	return pipelineDispatch(append([]string{content}, metadata...)...) == "unknown"
}

// executePipeline runs validated steps in order. A failing step stops the
// remaining steps unless it is marked on_error=continue or a later step
// handles it with if_failed; then only those recovery branches and
// independent continue steps run. Every step that does not run is reported
// as skipped with its reason.
func executePipeline(ctx context.Context, steps []PipelineStep, ret []string, call fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (run pipelineRun, runErr error) {
	run = pipelineRun{imageIndex: -1}
	var active *PipelineStep
	activeIteration := 0
	issued := false
	defer func() {
		if runErr == nil && run.invalid == "" && run.stopTurn == nil && !run.failed {
			return
		}
		reason := run.invalid
		if run.stopTurn != nil {
			reason = "waiting for user"
		}
		if reason == "" {
			reason = "an earlier step failed"
		}
		if runErr != nil {
			reason = runErr.Error()
			run.invalid = reason
		}
		run.failed = true
		seen := map[string]map[int]bool{}
		for _, r := range run.results {
			if seen[r.ID] == nil {
				seen[r.ID] = map[int]bool{}
			}
			seen[r.ID][r.Iteration] = true
		}
		for _, step := range steps {
			for i := range max(1, len(step.Items)) {
				if seen[step.ID][i] {
					continue
				}
				r := PipelineResult{ID: step.ID, Iteration: i, Status: StepSkipped, Outcome: OutcomeBlocked, Dispatch: "none", Reason: "not run: pipeline interrupted: " + reason}
				if ctx.Err() != nil {
					r.Outcome = OutcomeCanceled
				}
				if active != nil && active.ID == step.ID && i == activeIteration {
					r.Status = StepFailed
					r.IsError = true
					r.Content = reason
					r.ErrorCode = "pipeline_interrupted"
					if issued && !pipelineReadOnlyRecovery(step) {
						r.Status = StepUncertain
						r.Outcome = OutcomeUnknown
						r.Dispatch = "unknown"
					} else if ctx.Err() == nil {
						r.Outcome = OutcomeFailed
					}
				}
				run.results = append(run.results, r)
			}
		}
	}()
	success := map[string]bool{}
	failedSteps := map[string]bool{}
	recovered := map[string]bool{}
	snapshots := map[string]string{}
	outputs := map[string]json.RawMessage{}
	uncertain := map[string]bool{}
	observations := map[string]bool{}
	for _, s := range steps {
		observations[s.ID] = s.Tool == ComputerToolName && s.Arguments["action"] == "observe" && len(s.Items) == 0
	}
	var closing []string
	stoppedBy := ""
	unknownOutcome := false
	skip := func(s PipelineStep, reason string) {
		run.results = append(run.results, PipelineResult{ID: s.ID, Status: StepSkipped, Outcome: skippedOutcome(reason), Reason: reason})
	}
	for index, s := range steps {
		active = &s
		activeIteration = 0
		issued = false
		if err := ctx.Err(); err != nil {
			return run, err
		}
		switch {
		case unknownOutcome && !pipelineReadOnlyRecovery(s):
			skip(s, "not run: an earlier step has an unknown outcome; only read-only recovery may run")
			continue
		case stoppedBy != "" && (s.IfFailed == "" || !failedSteps[s.IfFailed]):
			skip(s, "not run: step "+stoppedBy+" failed")
			continue
		case s.IfSuccess != "" && !success[s.IfSuccess]:
			skip(s, "not run: if_success "+s.IfSuccess+" did not succeed")
			continue
		case s.IfFailed != "" && !failedSteps[s.IfFailed]:
			skip(s, "not run: if_failed "+s.IfFailed+" did not fail")
			continue
		}
		if s.When != nil {
			if run, reason := pipelineConditionMet(*s.When, outputs); !run {
				skip(s, "not run: "+reason)
				continue
			}
		}
		items := s.Items
		if len(items) == 0 {
			items = []string{""}
		}
		success[s.ID] = true
		iterationStart := len(run.results)
		for i, item := range items {
			activeIteration = i
			issued = false
			if err := ctx.Err(); err != nil {
				return run, err
			}
			arguments := substitutePipeline(s.Arguments, item)
			if err := bindPipelineArguments(arguments, s.Bindings, outputs); err != nil {
				run.results = append(run.results, PipelineResult{ID: s.ID, Iteration: i, Status: StepFailed, Outcome: OutcomeFailed, IsError: true, Dispatch: "none", Content: err.Error() + "; nothing was sent"})
				run.failed, success[s.ID], failedSteps[s.ID] = true, false, true
				delete(outputs, s.ID)
				handled := slices.ContainsFunc(steps[index+1:], func(next PipelineStep) bool { return next.IfFailed == s.ID })
				if s.OnError == "continue" && !handled {
					continue
				}
				stoppedBy = s.ID
				break
			}
			if s.ObservationFrom != "" {
				id := snapshots[s.ObservationFrom]
				if id == "" {
					run.invalid = "observation_from: missing snapshot_id; observe again"
					return run, nil
				}
				args, ok := arguments.(map[string]any)
				if !ok {
					run.invalid = "observation_from requires computer arguments"
					return run, nil
				}
				args["snapshot_id"] = id
			}
			encoded, err := json.Marshal(arguments)
			if err != nil {
				return run, err
			}
			if len(encoded) > 64*1024 {
				run.invalid = "pipeline arguments exceed limit"
				return run, nil
			}
			started := time.Now()
			issued = true
			response, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s/%s/%d", call.ID, s.ID, i), Name: s.Tool, Input: string(encoded)})
			if err != nil {
				if native, ok := ResponseOutcome(response); ok && native.Dispatch == "none" {
					issued = false
				}
				return run, err
			}
			attempts := 1
			if code := pipelineErrorCode(response); response.IsError && !response.StopTurn && pipelineMayRetry(s, code, response.Content, response.Metadata) {
				// One retry, only for a refusal before any input, and only
				// after a fresh observation gave the semantic target a
				// chance to resolve. The same input is never replayed after
				// it may have reached the page.
				issued = false
				observe, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s/%s/%d/observe", call.ID, s.ID, i), Name: BrowserToolName, Input: `{"action":"observe","advanced":{"mode":"delta"}}`})
				if err != nil {
					return run, err
				}
				if observe.StopTurn {
					response = observe
				} else if !observe.IsError {
					attempts = 2
					issued = true
					response, err = invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s/%s/%d/retry", call.ID, s.ID, i), Name: s.Tool, Input: string(encoded)})
					if err != nil {
						return run, err
					}
				}
			}
			elapsed := time.Since(started).Milliseconds()
			var desktopState *DesktopEvidence
			if s.Tool == ComputerToolName && !response.IsError && !response.StopTurn {
				var input ComputerParams
				if json.Unmarshal(encoded, &input) == nil {
					if input.Action == "hotkey" && strings.EqualFold(strings.TrimSpace(input.Modifiers), "alt") && strings.EqualFold(strings.TrimSpace(input.Key), "f4") && input.Automation.WindowID != "" && !slices.Contains(closing, input.Automation.WindowID) {
						closing = append(closing, input.Automation.WindowID)
					}
					desktopState = summarizeDesktopEvidence(input.Action, response.Content, closing)
				}
			}
			if !response.IsError && observations[s.ID] {
				var snapshot struct {
					ID string `json:"snapshot_id"`
				}
				if json.Unmarshal([]byte(response.Content), &snapshot) == nil {
					snapshots[s.ID] = snapshot.ID
				}
			}
			if s.RequirePassed && !response.IsError && !response.StopTurn {
				var assertion struct {
					Passed bool `json:"passed"`
				}
				if json.Unmarshal([]byte(response.Content), &assertion) != nil || !assertion.Passed {
					response = fantasy.NewTextErrorResponse("gate_failed: " + s.ID + "; expected passed:true; subsequent steps were not executed")
				}
			}
			content := response.Content
			if len(content) > pipelineStepContentLimit && !observations[s.ID] {
				content = strings.ToValidUTF8(content[:pipelineStepContentLimit], "") + "\n[truncated]"
			}
			metadata := response.Metadata
			if len(metadata) > 4000 {
				metadata = "[metadata omitted: exceeds 4 KiB]"
			}
			status := StepSucceeded
			switch {
			case response.StopTurn:
				status = StepStopped
			case response.IsError && pipelineStepUncertain(response.Content, response.Metadata):
				status = StepUncertain
			case response.IsError:
				status = StepFailed
			}
			result := PipelineResult{ID: s.ID, Iteration: i, Status: status, Outcome: pipelineStepOutcome(status, s, response), IsError: response.IsError, Content: content, Metadata: metadata, DesktopState: desktopState, DurationMS: elapsed}
			if native, ok := ResponseOutcome(response); ok {
				result.Dispatch = native.Dispatch
			}
			if attempts > 1 {
				result.Attempts = attempts
			}
			if response.IsError {
				result.ErrorCode = pipelineErrorCode(response)
				result.Dispatch = pipelineDispatch(response.Content, response.Metadata)
			}
			run.results = append(run.results, result)
			if response.Type == "image" && len(response.Data) > 0 {
				at := len(run.results) - 1
				run.results[at].MediaType = response.MediaType
				run.results[at].ImageOmitted = true
				if !response.IsError && (len(ret) == 0 || slices.Contains(ret, s.ID)) {
					if len(response.Data) > 8*1024*1024 {
						run.invalid = "pipeline image exceeds 8 MiB; request a smaller crop and observe current state before retrying"
						return run, nil
					}
					// Keep one selected image without retaining intermediate frames.
					run.imageData, run.imageMediaType, run.imageIndex = response.Data, response.MediaType, at
					run.imageProtected = protectedCredentialMedia(s.Tool, response)
					if run.imageProtected {
						run.imageProofHash = sha256.Sum256(response.Data)
						var meta struct {
							Proof credentialImageProof `json:"credential_image_redaction"`
						}
						_ = json.Unmarshal([]byte(response.Metadata), &meta)
						run.imageMasks = meta.Proof.Masks
					}
				}
			}
			if !response.IsError {
				// Every iteration replaces the exposed output, including a
				// successful result that has no bindable JSON.
				delete(outputs, s.ID)
				if out := pipelineOutput(response.Content); out != nil {
					outputs[s.ID] = out
				}
			}
			if response.StopTurn {
				r := response
				run.stopTurn = &r
				for _, rest := range steps[index+1:] {
					skip(rest, "not run: the turn stopped at "+s.ID)
				}
				return run, nil
			}
			if response.IsError {
				success[s.ID], failedSteps[s.ID] = false, true
				// A failed iteration must not leave the previous iteration's output
				// for later bindings to read as if it were current.
				delete(outputs, s.ID)
				uncertain[s.ID] = status == StepUncertain
				unknownOutcome = unknownOutcome || uncertain[s.ID]
				handled := slices.ContainsFunc(steps[index+1:], func(next PipelineStep) bool { return next.IfFailed == s.ID })
				// A step whose effect is unknown cannot be tolerated: later steps
				// would build on a state nobody observed.
				if s.OnError == "continue" && !handled && status != StepUncertain {
					// Tolerated independent failure: reported, not fatal.
					continue
				}
				run.failed = true
				stoppedBy = s.ID
				break
			}
		}
		if stoppedBy == s.ID {
			for i := len(run.results) - iterationStart; i < len(items); i++ {
				run.results = append(run.results, PipelineResult{ID: s.ID, Iteration: i, Status: StepSkipped, Outcome: OutcomeBlocked, Reason: "not run: an earlier iteration failed"})
			}
		}
		if s.IfFailed != "" && success[s.ID] && !uncertain[s.IfFailed] {
			// The recovery branch worked: the failure is handled and the
			// remaining steps run again.
			recovered[s.IfFailed] = true
			if stoppedBy == s.IfFailed {
				stoppedBy = ""
			}
		}
	}
	if run.failed {
		run.failed = false
		unresolved := map[string]bool{}
		for id := range failedSteps {
			step := steps[slices.IndexFunc(steps, func(s PipelineStep) bool { return s.ID == id })]
			if !recovered[id] && (step.OnError != "continue" || uncertain[id]) {
				run.failed = true
				unresolved[id] = true
			}
		}
		for _, result := range run.results {
			if unresolved[result.ID] && (result.Status == StepFailed || result.Status == StepUncertain) {
				if run.resumeStep == "" || result.Status == StepUncertain {
					run.resumeStep = result.ID
				}
				if result.Status == StepUncertain {
					break
				}
			}
		}
	}
	return run, nil
}

// Step outcomes shared by pipeline, browser workflow and desktop results.
const (
	OutcomePlanned    = "planned"
	OutcomeDispatched = "dispatched"
	OutcomeApplied    = "applied"
	OutcomeVerified   = "verified"
	OutcomeUnknown    = "unknown"
	OutcomeBlocked    = "blocked"
	OutcomeCanceled   = "canceled"
	OutcomeFailed     = "failed"
)

// skippedOutcome separates a step that was never due (its condition did not
// select it) from one blocked by an earlier failure, unknown effect or stop.
func skippedOutcome(reason string) string {
	for _, cause := range []string{"failed", "unknown outcome", "turn stopped", "earlier iteration"} {
		if strings.Contains(reason, cause) && !strings.Contains(reason, "did not fail") {
			return OutcomeBlocked
		}
	}
	return OutcomePlanned
}

// pipelineStepOutcome maps one executed step onto the shared states. A
// passed gate or a native verified result is verified; any other success is
// only applied, never proof of the requested result.
func pipelineStepOutcome(status string, s PipelineStep, response fantasy.ToolResponse) string {
	native, hasNative := ResponseOutcome(response)
	switch status {
	case StepUncertain:
		return OutcomeUnknown
	case StepStopped:
		if strings.Contains(strings.ToLower(response.Content), "cancel") {
			return OutcomeCanceled
		}
		return OutcomeBlocked
	case StepFailed:
		if hasNative && native.Dispatch == "unknown" {
			return OutcomeUnknown
		}
		if strings.HasPrefix(response.Content, "canceled") {
			return OutcomeCanceled
		}
		return OutcomeFailed
	}
	if s.RequirePassed || hasNative && native.Verified {
		return OutcomeVerified
	}
	var started struct {
		JobID string `json:"job_id"`
	}
	if json.Unmarshal([]byte(response.Content), &started) == nil && started.JobID != "" {
		return OutcomeDispatched
	}
	return OutcomeApplied
}

// pipelineOutput returns the JSON an earlier step exposes to bindings: the
// result itself when it is JSON, or a screenshot's viewport grant.
func pipelineOutput(content string) json.RawMessage {
	if json.Valid([]byte(content)) && len(content) <= 64*1024 {
		return json.RawMessage(content)
	}
	if _, rest, ok := strings.Cut(content, "\nViewport: "); ok {
		line, _, _ := strings.Cut(rest, "\n")
		if json.Valid([]byte(line)) {
			return json.RawMessage(line)
		}
	}
	return nil
}

func pipelineResponse(run pipelineRun, ret []string) (fantasy.ToolResponse, error) {
	if run.stopTurn != nil {
		annotateResume(run.results, run.resumeStep)
		resp := *run.stopTurn
		prefix := pipelineShort(resp.Content, 4096) + "\nPipeline results (already applied steps must not be replayed): "
		data, err := boundedPipelineJSONBudget(run.results, maxPipelineOutput-len(prefix))
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		resp.Content = prefix + string(data)
		return resp, nil
	}
	if run.invalid != "" {
		run.failed = true
		if len(run.results) > 0 {
			run.results[len(run.results)-1].Reason = "pipeline halted: " + run.invalid
		}
	}

	if run.failed {
		annotateResume(run.results, run.resumeStep)
		data, err := boundedPipelineJSON(run.results)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return fantasy.NewTextErrorResponse(string(data)), nil
	}
	if run.imageIndex >= 0 {
		run.results[run.imageIndex].ImageAttached = true
		run.results[run.imageIndex].ImageOmitted = false
	}
	returned := run.results
	if len(ret) > 0 {
		returned = nil
		for _, r := range run.results {
			if slices.Contains(ret, r.ID) && r.Status != StepSkipped {
				returned = append(returned, r)
			}
		}
	}
	data, err := boundedPipelineJSON(returned)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if run.imageIndex >= 0 {
		if run.imageProtected && run.imageProofHash != sha256.Sum256(run.imageData) {
			return fantasy.NewTextErrorResponse("credential_redaction_image_changed: the selected protected image changed before delivery"), nil
		}
		response := fantasy.NewImageResponse(run.imageData, run.imageMediaType)
		response.Content = string(data)
		if run.imageProtected {
			response = withCredentialImageProof(response, run.imageMasks)
		}
		return response, nil
	}
	return fantasy.NewTextResponse(string(data)), nil
}

// maxPipelineOutput bounds the JSON returned to the model.
const maxPipelineOutput = 64 * 1024

// boundedPipelineJSON keeps every step's identity, status, outcome and
// dispatch within the output bound. Large contents are shortened first, then
// dropped, so an oversized result never hides which steps already took effect.
// Before, the whole ledger was replaced by an error after the steps had run.
func boundedPipelineJSON(results []PipelineResult) ([]byte, error) {
	return boundedPipelineJSONBudget(results, maxPipelineOutput)
}

func boundedPipelineJSONBudget(results []PipelineResult, budget int) ([]byte, error) {
	data, err := json.Marshal(results)
	if err != nil || len(data) <= budget {
		return data, err
	}
	compact := slices.Clone(results)
	for _, limit := range []int{2048, 512, 0} {
		for i := range compact {
			original := results[i].Content
			compact[i].Metadata = ""
			if len(original) <= limit {
				continue
			}
			note := fmt.Sprintf("[%d bytes omitted to fit the 64 KiB pipeline output; observe again if needed]", len(original)-limit)
			compact[i].Content = strings.ToValidUTF8(original[:limit], "") + "\n" + note
			if limit == 0 {
				compact[i].Content = note
			}
		}
		if data, err = json.Marshal(compact); err != nil || len(data) <= budget {
			return data, err
		}
	}
	for i := range compact {
		r := &compact[i]
		r.DesktopState = nil
		r.Content = "[details omitted; observe current state before retrying]"
		r.Metadata = ""
		r.Reason = pipelineShort(r.Reason, 96)
		r.Resume = pipelineShort(r.Resume, 256)
		r.ErrorCode = pipelineShort(r.ErrorCode, 64)
		r.MediaType = pipelineShort(r.MediaType, 64)
	}
	data, err = json.Marshal(compact)
	if err != nil || len(data) <= budget {
		return data, err
	}
	for i := range compact {
		r := compact[i]
		compact[i] = PipelineResult{ID: r.ID, Iteration: r.Iteration, Status: r.Status, Outcome: r.Outcome, IsError: r.IsError, Dispatch: r.Dispatch, ImageAttached: r.ImageAttached, ImageOmitted: r.ImageOmitted}
	}
	data, err = json.Marshal(compact)
	if err == nil && len(data) > budget {
		return nil, fmt.Errorf("pipeline identity ledger exceeds output budget")
	}
	return data, err
}

func pipelineShort(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "") + "[truncated]"
}

// annotateResume describes dispatch and progress per iteration, so a failed
// or partially completed items step is never suggested for wholesale replay.
func annotateResume(results []PipelineResult, resumeStep string) {
	var done, skipped []string
	counts := map[string]int{}
	for _, r := range results {
		counts[r.ID]++
	}
	label := func(r PipelineResult) string {
		if counts[r.ID] > 1 || r.Iteration > 0 {
			return fmt.Sprintf("%s iteration %d", r.ID, r.Iteration)
		}
		return r.ID
	}
	stop := -1
	for i, r := range results {
		switch r.Status {
		case StepSucceeded:
			if id := label(r); !slices.Contains(done, id) {
				done = append(done, id)
			}
		case StepSkipped:
			skipped = append(skipped, label(r))
		case StepFailed, StepUncertain:
			if stop < 0 && (resumeStep == "" || r.ID == resumeStep) {
				stop = i
			}
		}
	}
	if stop < 0 {
		return
	}
	failed := results[stop]
	var b strings.Builder
	if len(done) > 0 {
		fmt.Fprintf(&b, "Already applied, do not repeat: %s. ", strings.Join(done, ", "))
	} else {
		b.WriteString("No successful iterations were recorded. ")
	}
	dispatch := failed.Dispatch
	if dispatch == "" {
		dispatch = pipelineDispatch(failed.Content, failed.Metadata)
	}
	switch {
	case failed.Status == StepUncertain || dispatch == "unknown":
		fmt.Fprintf(&b, "Step %s has an unknown effect: inspect the target and reconcile this iteration before any further mutation or resend. ", label(failed))
	case dispatch == "sent" || dispatch == "partial":
		fmt.Fprintf(&b, "Step %s sent input and may have partial effects: inspect what changed before choosing any remaining work; do not replay this iteration blindly. ", label(failed))
	case dispatch == "none":
		fmt.Fprintf(&b, "Step %s was refused before dispatch; fix its cause (see content) and observe again for fresh refs. ", label(failed))
	default:
		fmt.Fprintf(&b, "Step %s failed without dispatch evidence: inspect its effects before deciding whether to resend this iteration. ", label(failed))
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "Not run: %s. After resolving the failure, create a new call containing only the remaining iterations and steps; never replay the full items list.", strings.Join(skipped, ", "))
	}
	results[stop].Resume = strings.TrimSpace(b.String())
}

func substitutePipeline(v any, item string) any {
	switch value := v.(type) {
	case string:
		if value == "$item" {
			return item
		}
		return value
	case map[string]any:
		out := map[string]any{}
		for k, v := range value {
			out[k] = substitutePipeline(v, item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = substitutePipeline(v, item)
		}
		return out
	default:
		return v
	}
}
