package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// DesktopWorkflowParams groups known operations while preserving child controls.
type DesktopWorkflowParams struct {
	FocusWindow   bool                       `json:"focus_window,omitempty" description:"For act/fill_submit: activate the explicit input window and confirm its native identity before input; never chooses a different window."`
	Rename        *DesktopRenameParams       `json:"rename,omitempty" description:"For rename: exact observed Explorer item name and replacement display name; selection, rename editor and final item are verified in one call."`
	Flow          *DesktopFlowParams         `json:"flow,omitempty" description:"For flow: bounded branching graph with live window bindings, targeted checkpoints and optional durable resume."`
	Result        string                     `json:"result,omitempty" description:"For sequence only: observe (default) or checkpoints. Checkpoints returns actual verified readbacks and fresh final foreground without screenshots; only use for already-resolved operations."`
	AdaptiveSteps []DesktopAdaptiveStep      `json:"adaptive_steps,omitempty" description:"For adaptive: 1-8 conditional operations with mandatory result checkpoints; accessibility method selection happens before input."`
	Steps         []DesktopWorkflowStep      `json:"steps,omitempty" description:"For sequence: 1-24 known scoped inputs, each with a required checkpoint."`
	Transition    *DesktopTransitionParams   `json:"transition,omitempty" description:"For transition: expected dialog title or application identity after one input."`
	Observation   string                     `json:"observation,omitempty" description:"Observation mode: auto (recipe default), semantic, or explicit visual."`
	Mode          string                     `json:"mode" enum:"observe,prepare,act,fill_submit,sequence,adaptive,transition,flow,rename" description:"observe: read an explicit window without focus or input; prepare: find/launch/focus/observe; rename: verified Explorer item rename in one call; act: input and observe; fill_submit: verify field and submit; sequence: deterministic inputs and checkpoints; adaptive: conditional steps and live method selection; transition: identify expected related window; flow: branching graph, dynamic window bindings, targeted verification and durable resume."`
	Application   string                     `json:"application,omitempty" description:"Installed application name for prepare. Localized Calculator names, process-identified Notepad and class-verified File Explorer folder windows are supported; other open titles match exactly. Launch also accepts an installed AppID. Use window_id for unresolved or ambiguous windows."`
	WindowID      string                     `json:"window_id,omitempty" description:"Explicit observed window ID for prepare, avoiding title ambiguity."`
	Input         ComputerParams             `json:"input,omitempty" description:"Single known input for act; omit when using inputs. For fill_submit use set_value with text and a fresh field selector. window_id required."`
	Inputs        []ComputerParams           `json:"inputs,omitempty" description:"For act or transition: 1-16 already-resolved same-window inputs, then one observation or expected window transition. Omit input entirely; do not duplicate the first entry."`
	WaitFor       computer.AutomationRequest `json:"wait_for,omitempty" description:"Optional expected-state assertion after input, before observation. Same window only."`
	WaitMS        int                        `json:"wait_ms,omitempty" description:"prepare launch wait, default 8000, maximum 15000."`
	MaxElements   int                        `json:"max_elements,omitempty" description:"Initial observation limit, default 80, maximum 500. Auto may expand a small truncated scan once to 150 to avoid unnecessary screenshots."`
}

func runDesktopWorkflow(ctx context.Context, p DesktopWorkflowParams, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	if p.FocusWindow && p.Mode != "act" && p.Mode != "fill_submit" {
		return fantasy.NewTextErrorResponse("focus_window requires act/fill_submit mode"), nil
	}
	if p.Mode == "observe" {
		return runDesktopObserve(ctx, p, parent, invoke)
	}
	if p.Mode == "rename" {
		return runDesktopRename(ctx, p, parent, invoke)
	}
	if p.Rename != nil {
		return fantasy.NewTextErrorResponse("rename requires rename mode"), nil
	}
	if p.Mode == "flow" {
		return runDesktopFlow(ctx, p, parent, invoke)
	}
	if p.Flow != nil {
		return fantasy.NewTextErrorResponse("flow requires flow mode"), nil
	}
	if p.Result != "" && (p.Mode != "sequence" || (p.Result != "observe" && p.Result != "checkpoints")) {
		return fantasy.NewTextErrorResponse("result requires sequence and observe/checkpoints"), nil
	}
	if p.Result == "checkpoints" && p.Observation == "visual" {
		return fantasy.NewTextErrorResponse("checkpoints result cannot request visual observation"), nil
	}
	if p.Observation == "" {
		p.Observation = "auto"
	}
	invoke = desktopRecoveryInvoke(ctx, parent, invoke)
	if p.Observation != "" && p.Observation != "visual" && p.Observation != "semantic" && p.Observation != "auto" {
		return fantasy.NewTextErrorResponse("invalid desktop observation mode"), nil
	}
	if p.WaitMS < 0 || p.WaitMS > 15000 || p.MaxElements < 0 || p.MaxElements > 500 {
		return fantasy.NewTextErrorResponse("invalid desktop workflow bounds"), nil
	}
	if p.Mode == "adaptive" {
		return runDesktopAdaptive(ctx, p, parent, invoke)
	}
	if len(p.AdaptiveSteps) > 0 {
		return fantasy.NewTextErrorResponse("adaptive_steps requires adaptive mode"), nil
	}
	if p.Mode == "sequence" {
		return runDesktopSequence(ctx, p, parent, invoke)
	}
	if p.Mode == "transition" {
		return runDesktopTransition(ctx, p, parent, invoke)
	}
	if len(p.Steps) != 0 || p.Transition != nil {
		return fantasy.NewTextErrorResponse("steps and transition require their corresponding mode"), nil
	}
	p.Application = strings.TrimSpace(p.Application)
	var knownInputs []ComputerParams
	if len(p.Inputs) > 0 {
		if p.Mode != "act" {
			return fantasy.NewTextErrorResponse("inputs requires act mode"), nil
		}
		var err error
		knownInputs, err = desktopKnownInputs(p.Input, p.Inputs)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		p.Input = knownInputs[0]
	}
	if p.Mode != "prepare" && p.Mode != "act" && p.Mode != "fill_submit" {
		return invalidDesktopMode(p.Mode), nil
	}
	if p.WaitFor.WindowID != "" && p.WaitFor.WindowID != p.Input.Automation.WindowID {
		return fantasy.NewTextErrorResponse("wait_for must target the input window"), nil
	}
	if err := computer.ValidateAutomationRequest(p.WaitFor); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if p.WaitFor.Condition != "" {
		if p.WaitFor.ElementID == "" && p.WaitFor.Name == "" && p.WaitFor.Role == "" {
			return fantasy.NewTextErrorResponse("wait_for requires an explicit expected-state target"), nil
		}
		switch p.WaitFor.Condition {
		case "visible", "hidden", "enabled", "text", "value", "document_text", "keyboard_focused":
		default:
			return fantasy.NewTextErrorResponse("unsupported wait_for condition"), nil
		}
	}
	if p.Mode == "prepare" && (p.WaitFor.Condition != "" || p.Input.Action != "") {
		return fantasy.NewTextErrorResponse("prepare cannot include input or wait_for"), nil
	}
	if p.Mode != "prepare" && (p.Input.Automation.WindowID == "" || !desktopMutation(p.Input.Action) || p.Input.Action == "focus" || p.Input.Action == "launch_app") {
		return fantasy.NewTextErrorResponse("desktop input requires a mutation and explicit window_id"), nil
	}
	if p.Mode != "prepare" && p.WindowID != "" && p.WindowID != p.Input.Automation.WindowID {
		return fantasy.NewTextErrorResponse("window_id conflicts with scoped desktop input; no input sent"), nil
	}
	if p.Mode == "fill_submit" && (p.Input.Action != "set_value" || p.Input.Automation.Text == "") {
		return fantasy.NewTextErrorResponse("fill_submit requires set_value with automation.text"), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	actions := []string{}
	operationCtx := ctx
	child := func(input ComputerParams) (fantasy.ToolResponse, error) {
		if err := operationCtx.Err(); err != nil {
			return fantasy.ToolResponse{}, err
		}
		data, err := json.Marshal(input)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if len(data) > 64*1024 {
			return fantasy.NewTextErrorResponse("desktop child arguments exceed limit"), nil
		}
		actions = append(actions, input.Action)
		return invoke(operationCtx, fantasy.ToolCall{ID: fmt.Sprintf("%s/desktop/%d", parent.ID, len(actions)), Name: ComputerToolName, Input: string(data)})
	}
	stopped := func(r fantasy.ToolResponse, err error) bool { return err != nil || r.IsError || r.StopTurn }
	id := p.Input.Automation.WindowID
	var propertiesBaseline []desktopWindowInfo
	var refreshWindow func() (string, fantasy.ToolResponse, error)
	if p.Mode == "prepare" {
		if strings.TrimSpace(p.Application) == "" && p.WindowID == "" {
			return fantasy.NewTextErrorResponse("prepare requires application or explicit window_id"), nil
		}
		find := func() (string, fantasy.ToolResponse, error) {
			r, err := child(ComputerParams{Action: "windows"})
			if stopped(r, err) {
				return "", r, err
			}
			var envelope struct {
				Result []desktopWindowInfo `json:"result"`
			}
			if json.Unmarshal([]byte(r.Content), &envelope) != nil {
				return "", fantasy.NewTextErrorResponse("Invalid window list"), nil
			}
			matches := []string{}
			foreground := ""
			for _, w := range envelope.Result {
				if (p.WindowID != "" && w.ID == p.WindowID) || (p.WindowID == "" && desktopApplicationWindowMatches(w, p.Application)) {
					matches = append(matches, w.ID)
					if w.Foreground {
						foreground = w.ID
					}
				}
			}
			if len(matches) > 1 {
				if foreground != "" {
					return foreground, r, nil
				}
				candidates := make([]desktopWindowInfo, 0, len(matches))
				for _, w := range envelope.Result {
					if desktopApplicationWindowMatches(w, p.Application) {
						candidates = append(candidates, w)
					}
				}
				data, _ := json.Marshal(candidates)
				return "", fantasy.NewTextErrorResponse("ambiguous_target: multiple application windows; inspect an explicit window_id without closing or relaunching an unverified existing window. Candidates: " + string(data)), nil
			}
			if len(matches) == 1 {
				return matches[0], r, nil
			}
			return "", r, nil
		}
		refreshWindow = find
		var r fantasy.ToolResponse
		var err error
		id, r, err = find()
		if stopped(r, err) {
			return r, err
		}
		if id == "" {
			if p.WindowID != "" {
				return fantasy.NewTextErrorResponse("target_missing: supplied window disappeared"), nil
			}
			r, err = child(ComputerParams{Action: "launch_app", Automation: computer.AutomationRequest{Name: p.Application}})
			if stopped(r, err) {
				return r, err
			}
			var launched struct {
				Result struct {
					Application string `json:"application"`
				} `json:"result"`
			}
			if json.Unmarshal([]byte(r.Content), &launched) == nil && launched.Result.Application != "" {
				p.Application = launched.Result.Application
			}
			wait := p.WaitMS
			if wait == 0 {
				wait = 8000
			}
			waitCtx, stop := context.WithTimeout(ctx, time.Duration(wait)*time.Millisecond)
			defer stop()
			operationCtx = waitCtx
			for id == "" {
				select {
				case <-waitCtx.Done():
					return fantasy.NewTextErrorResponse("condition_timeout: application window did not appear; inspect windows before retrying launch"), nil
				case <-time.After(300 * time.Millisecond):
				}
				id, r, err = find()
				if stopped(r, err) {
					return r, err
				}
			}
			operationCtx = ctx
		}
		r, err = child(ComputerParams{Action: "focus", Automation: computer.AutomationRequest{WindowID: id}})
		var focusEvidence fantasy.ToolResponse
		// A replaced application handle may invalidate activation. Re-resolve once,
		// only for automatic selection and platform target/activation errors.
		if err == nil && r.IsError && !r.StopTurn && p.WindowID == "" && (strings.HasPrefix(r.Content, "focus_denied:") || strings.HasPrefix(r.Content, "target_missing:")) {
			next, listed, listErr := find()
			if stopped(listed, listErr) {
				return listed, listErr
			}
			focusEvidence = listed
			if next != "" && next != id {
				id = next
				r, err = child(ComputerParams{Action: "focus", Automation: computer.AutomationRequest{WindowID: id}})
				focusEvidence = fantasy.ToolResponse{}
			}
		}
		if stopped(r, err) {
			if err == nil && r.IsError && !r.StopTurn && ctx.Err() == nil && strings.HasPrefix(r.Content, "focus_denied:") {
				if focusEvidence.Content == "" {
					_, listed, listErr := find()
					if stopped(listed, listErr) {
						return listed, listErr
					}
					focusEvidence = listed
				}
				r = desktopFocusEvidence(r, id, focusEvidence.Content)
			}
			return r, err
		}
		var focus struct {
			Result struct {
				Focused bool `json:"focused"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(r.Content), &focus) != nil || !focus.Result.Focused {
			return fantasy.NewTextErrorResponse("focus_denied: application focus was not confirmed"), nil
		}
	} else {
		if p.FocusWindow {
			windows, r, err := desktopFlowWindows(ctx, func(ctx context.Context, input ComputerParams) (fantasy.ToolResponse, error) { return child(input) })
			if stopped(r, err) {
				return r, err
			}
			var window desktopWindowInfo
			for _, candidate := range windows {
				if candidate.ID == id && candidate.ProcessID > 0 && candidate.WindowClass != "" {
					window = candidate
				}
			}
			if window.ID == "" {
				return fantasy.NewTextErrorResponse("target_missing: focus_window requires a fresh native identity; no input sent"), nil
			}
			r, err = desktopFlowIdentity(ctx, window, true, func(ctx context.Context, input ComputerParams) (fantasy.ToolResponse, error) { return child(input) })
			if stopped(r, err) {
				return r, err
			}
		}
		input := p.Input
		if p.Mode == "fill_submit" {
			input.Automation.Focus = true
		}
		if len(knownInputs) == 0 {
			knownInputs = []ComputerParams{input}
		}
		if desktopPropertiesShortcut(knownInputs[len(knownInputs)-1]) {
			listed, listErr := child(ComputerParams{Action: "windows"})
			if stopped(listed, listErr) {
				return listed, listErr
			}
			propertiesBaseline = desktopPropertiesWindows(listed.Content)
		}
		var r fantasy.ToolResponse
		var err error
		for _, known := range knownInputs {
			r, err = child(known)
			if stopped(r, err) {
				return r, err
			}
		}
		if p.Mode == "fill_submit" {
			var field struct {
				Result struct {
					ElementID       string `json:"element_id"`
					ValueVerified   bool   `json:"value_verified"`
					KeyboardFocused bool   `json:"keyboard_focused"`
				} `json:"result"`
			}
			if json.Unmarshal([]byte(r.Content), &field) != nil || !field.Result.ValueVerified || !field.Result.KeyboardFocused || field.Result.ElementID == "" {
				return fantasy.NewTextErrorResponse("field_not_ready: text or keyboard focus unverified; Enter was not sent"), nil
			}
			r, err = child(ComputerParams{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: id, ElementID: field.Result.ElementID}})
			if stopped(r, err) {
				return r, err
			}
			knownInputs = append(knownInputs, ComputerParams{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: id}})
		}
		if p.WaitFor.Condition != "" {
			assertion := p.WaitFor
			assertion.WindowID = id
			r, err = child(ComputerParams{Action: "assert", Automation: assertion})
			if stopped(r, err) {
				return r, err
			}
			var state struct {
				Passed bool `json:"passed"`
			}
			if json.Unmarshal([]byte(r.Content), &state) != nil || !state.Passed {
				return fantasy.NewTextErrorResponse("gate_failed: expected application state not confirmed"), nil
			}
		}
	}
	if p.Mode != "prepare" && p.WaitFor.Condition == "" && len(knownInputs) > 0 {
		if outcome, handled, err := desktopWindowOutcome(ctx, id, knownInputs[len(knownInputs)-1], p.Mode, child); handled {
			return outcome, err
		}
	}
	limit := p.MaxElements
	if limit == 0 {
		limit = 80
	}
	r, err := child(ComputerParams{Action: "observe", Observation: p.Observation, Automation: computer.AutomationRequest{WindowID: id, MaxElements: limit}})
	if stopped(r, err) {
		if err == nil && r.IsError && !r.StopTurn && ctx.Err() == nil && p.Mode != "prepare" && p.WaitFor.Condition == "" && len(knownInputs) > 0 && (strings.HasPrefix(r.Content, "target_missing:") || strings.HasPrefix(r.Content, "accessibility_unavailable:")) {
			var recovery struct {
				Desktop struct {
					Windows []desktopWindowInfo `json:"fresh_windows"`
				} `json:"desktop_recovery"`
			}
			if json.Unmarshal([]byte(r.Metadata), &recovery) == nil && recovery.Desktop.Windows != nil {
				data, marshalErr := json.Marshal(map[string]any{"result": recovery.Desktop.Windows})
				if marshalErr == nil {
					outcome, handled, outcomeErr := desktopWindowOutcomeFromList(id, knownInputs[len(knownInputs)-1], p.Mode, fantasy.NewTextResponse(string(data)))
					if handled && outcomeErr == nil && !outcome.IsError {
						return outcome, nil
					}
				}
			}
		}
		return r, err
	}
	var observed map[string]any
	if json.Unmarshal([]byte(r.Content), &observed) != nil {
		return fantasy.NewTextErrorResponse("Invalid final desktop observation; do not replay the input"), nil
	}
	// Re-observe shell-only trees after startup without replaying any input.
	// Explicit handles stay pinned; automatic selection uses a fresh window list.
	retries := 0
	for p.Mode == "prepare" && desktopShellOnly(observed) && retries < 2 {
		select {
		case <-ctx.Done():
			return fantasy.ToolResponse{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		next, listed, listErr := refreshWindow()
		if stopped(listed, listErr) {
			return listed, listErr
		}
		if next == "" {
			return fantasy.NewTextErrorResponse("target_missing: application window disappeared during preparation; list windows again"), nil
		}
		focused, focusErr := child(ComputerParams{Action: "focus", Automation: computer.AutomationRequest{WindowID: next}})
		if stopped(focused, focusErr) {
			return focused, focusErr
		}
		var focus struct {
			Result struct {
				Focused bool `json:"focused"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(focused.Content), &focus) != nil || !focus.Result.Focused {
			return fantasy.NewTextErrorResponse("focus_denied: refreshed application focus was not confirmed"), nil
		}
		id = next
		r, err = child(ComputerParams{Action: "observe", Observation: p.Observation, Automation: computer.AutomationRequest{WindowID: id, MaxElements: limit}})
		if stopped(r, err) {
			return r, err
		}
		observed = nil
		if json.Unmarshal([]byte(r.Content), &observed) != nil {
			return fantasy.NewTextErrorResponse("Invalid refreshed desktop observation"), nil
		}
		retries++
	}
	if p.Mode == "prepare" && desktopShellOnly(observed) {
		observed["accessibility_status"] = "shell_only"
		observed["recovery"] = "Application controls were not observed after bounded retries. Inspect the returned screenshot or OCR; do not infer controls from the window frame."
	}
	desktopCheckpointGuidance(observed, id)
	var adapterWindow struct {
		Foreground *desktopWindowInfo `json:"foreground_window"`
	}
	if json.Unmarshal([]byte(r.Content), &adapterWindow) == nil && adapterWindow.Foreground != nil && adapterWindow.Foreground.ID == id {
		observed["application_adapter"] = desktopAdapterForWindow(*adapterWindow.Foreground)
	}
	inputWindow := id
	if p.Mode != "prepare" {
		var source struct {
			Foreground *desktopWindowInfo `json:"foreground_window"`
		}
		_ = json.Unmarshal([]byte(r.Content), &source)
		w := source.Foreground
		owned := w != nil && w.Owner == id
		if !owned && propertiesBaseline != nil && w != nil && w.ID != id && w.Foreground && !w.Minimized && w.WindowClass == "#32770" {
			listed, listErr := child(ComputerParams{Action: "windows"})
			if stopped(listed, listErr) {
				listed.Content += "\nThe preceding input was already executed; do not replay it."
				return listed, listErr
			}
			owned = desktopPropertiesRelated(id, *w, propertiesBaseline, desktopPropertiesWindows(listed.Content))
		}
		// Read a directly owned foreground dialog without redirecting any input.
		// New snapshot controls and any image must describe the same dialog.
		if w != nil && w.ID != "" && w.ID != id && owned && w.Foreground && !w.Minimized && w.WindowClass == "#32770" {
			dialog, dialogErr := child(desktopRecipeObservation(p, w.ID))
			if stopped(dialog, dialogErr) {
				dialog.Content += "\nThe preceding input was already executed; do not replay it."
				return dialog, dialogErr
			}
			var current map[string]any
			if json.Unmarshal([]byte(dialog.Content), &current) != nil || current["window_id"] != w.ID {
				return fantasy.NewTextErrorResponse("Invalid owned-dialog observation identity; preceding input was executed, do not replay it"), nil
			}
			observed, r, id = current, dialog, w.ID
			observed["source_window_id"] = inputWindow
			observed["related_foreground_observed"] = true
		}
	}
	observed["workflow"] = map[string]any{"mode": p.Mode, "window_id": id, "actions": actions, "condition_verified": p.WaitFor.Condition != "", "task_completion_requires_verification": true}
	if inputWindow != id {
		workflow := observed["workflow"].(map[string]any)
		workflow["input_window_id"] = inputWindow
		if p.WaitFor.Condition != "" {
			workflow["condition_window_id"] = inputWindow
		}
	}
	if evidence := summarizeDesktopEvidence("observe", r.Content, nil); evidence != nil {
		observed["desktop_state"] = evidence
	}
	data, err := marshalDesktopResult(observed)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if len(data) > 64*1024 {
		return fantasy.NewTextErrorResponse("Desktop observation exceeds 64 KiB; request a smaller observation without replaying input"), nil
	}
	r.Content = string(data)
	return r, nil
}
