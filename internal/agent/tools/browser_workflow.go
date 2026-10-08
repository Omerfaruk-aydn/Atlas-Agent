package tools

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
)

// BrowserWorkflowParams compiles to ordinary hooked browser calls. There is
// no second execution engine or permission bypass inside a browser session.
type BrowserWorkflowParams struct {
	TabID  string                `json:"tab_id,omitempty"`
	Origin string                `json:"origin,omitempty"`
	Steps  []BrowserWorkflowStep `json:"steps"`
}

type BrowserWorkflowStep struct {
	Bindings  map[string]string  `json:"bindings,omitempty" description:"Bind verified earlier JSON outputs, e.g. advanced.tab_id: popup#/tab_id, advanced.viewport_id: shot#/viewport_id, ref: found#/matches/0/ref (only when exactly one matched) or advanced.newer_than: download#/started_at."`
	ID        string             `json:"id"`
	Action    string             `json:"action"`
	Target    browser.Request    `json:"target,omitempty"`
	Ref       string             `json:"ref,omitempty" description:"Element ref from the latest observation, for click/type/set_field/read_field."`
	URL       string             `json:"url,omitempty"`
	Text      string             `json:"text,omitempty"`
	Key       string             `json:"key,omitempty" description:"For key: a key or chord such as escape, f, ctrl+a or shift+1."`
	Direction string             `json:"direction,omitempty" description:"For scroll: up, down, left or right."`
	Amount    int                `json:"amount,omitempty" description:"For scroll: pixels."`
	Annotate  *bool              `json:"annotate,omitempty" description:"For screenshot: false captures a clean image for validation or delivery, without issuing a pointer viewport grant."`
	Verify    *browser.Request   `json:"verify,omitempty" description:"Optional DOM postcondition; failure stops subsequent steps without replaying the action."`
	IfSuccess string             `json:"if_success,omitempty" description:"Run only when this earlier step succeeded."`
	IfFailed  string             `json:"if_failed,omitempty" description:"Run only when this earlier step failed (recovery branch); a successful branch resumes the workflow."`
	OnError   string             `json:"on_error,omitempty" description:"stop (default) or continue for an independent step whose failure later steps do not depend on."`
	When      *PipelineCondition `json:"when,omitempty" description:"Run only if an earlier step's result matches, e.g. {\"source\":\"found#/count\",\"equals\":\"0\"}."`
	RetryOn   []string           `json:"retry_on,omitempty" description:"For semantic targets only: target_not_found, target_resolution_failed, condition_timeout or stale_ref allow one retry after a fresh observe when nothing was sent."`
}

// maxWorkflowSteps bounds one browser workflow: room for a complete
// multi-section build with verification steps, still one bounded call.
const maxWorkflowSteps = 64

// workflowActions are the browser actions a workflow may compile to. eval,
// cdp and handoff are excluded: no generated scripts, no raw protocol and
// no control transfer inside an unattended sequence.
var workflowActions = map[string]bool{
	"navigate": true, "back": true, "forward": true, "click": true, "type": true, "key": true, "scroll": true,
	"semantic_click": true, "semantic_type": true, "find": true, "assert": true, "wait_for": true,
	"tabs": true, "tab_new": true, "tab_select": true, "tab_close": true, "tab_return": true, "frames": true,
	"upload": true, "download_start": true, "download_wait": true, "popup_wait": true, "dialog_wait": true, "dialog_handle": true,
	"observe": true, "screenshot": true, "set_field": true, "read_field": true, "select_option": true, "fill_form": true,
	"submit": true, "collect": true, "health": true,
	browser.InputPointerMove: true, browser.InputPointerClick: true, browser.InputPointerDrag: true, browser.InputKeyCombo: true, browser.InputInsertText: true,
}

func compileBrowserWorkflow(p BrowserWorkflowParams) ([]PipelineStep, error) {
	if len(p.Steps) < 1 || len(p.Steps) > maxWorkflowSteps {
		return nil, fmt.Errorf("browser workflow requires 1-%d steps (verification steps are added per step; maximum %d calls)", maxWorkflowSteps, maxPipelineInvocations)
	}
	seen := map[string]bool{}
	var steps []PipelineStep
	for _, step := range p.Steps {
		if step.ID == "" || seen[step.ID] || seen[step.ID+"/verify"] || strings.Contains(step.ID, "#") {
			return nil, fmt.Errorf("invalid or duplicate browser step id")
		}
		seen[step.ID], seen[step.ID+"/verify"] = true, true
		if !workflowActions[step.Action] {
			return nil, fmt.Errorf("unsupported browser workflow action %q", step.Action)
		}
		if step.Action == "navigate" {
			u, err := url.Parse(step.URL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return nil, fmt.Errorf("navigate requires an HTTP(S) URL")
			}
		}
		key := step.Key
		if step.Action == "key" || (step.Action == browser.InputKeyCombo && key != "") {
			normalized, modifiers, err := browser.NormalizeKey(firstNonEmpty(key, step.Target.Key))
			if err != nil {
				return nil, fmt.Errorf("step %s: %w", step.ID, err)
			}
			if step.Action == browser.InputKeyCombo {
				key = normalized
				step.Target.Modifiers = append(step.Target.Modifiers, modifiers...)
			}
		}
		if step.Ref != "" && strings.ContainsAny(step.Ref, `"[]`) {
			return nil, fmt.Errorf("step %s: invalid ref", step.ID)
		}
		target := step.Target
		target.Action = step.Action
		if target.ExpectedTabID == "" {
			target.ExpectedTabID = p.TabID
		}
		if target.ExpectedOrigin == "" {
			target.ExpectedOrigin = p.Origin
		}
		if target.Text == "" {
			target.Text = step.Text
		}
		if target.Key == "" && step.Action == browser.InputKeyCombo {
			target.Key = key
		}
		validation := target
		if step.Bindings["advanced.tab_id"] != "" {
			validation.TabID = "bound"
		}
		if step.Bindings["advanced.newer_than"] != "" {
			validation.NewerThan = time.Unix(1, 0)
		}
		if step.Bindings["advanced.viewport_id"] != "" {
			validation.ViewportID = "bound"
		}
		if step.Ref != "" || step.Bindings["ref"] != "" {
			validation.Selector = "[data-atlas-ref]"
		}
		switch step.Action {
		case "click", "type":
			if step.Ref == "" && step.Bindings["ref"] == "" && target.Selector == "" {
				return nil, fmt.Errorf("step %s: %s requires ref (from an observation or a bound find) or target.selector; for target.role/name/label use semantic_click or semantic_type", step.ID, step.Action)
			}
		case "scroll":
			if _, _, err := scrollDelta(step.Direction, step.Amount); err != nil {
				return nil, fmt.Errorf("step %s: %w", step.ID, err)
			}
		}
		if err := browser.ValidateWorkflowRequest(validation); err != nil {
			return nil, fmt.Errorf("step %s: %w", step.ID, err)
		}
		arguments, err := browserWorkflowArguments(step, target, key)
		if err != nil {
			return nil, err
		}
		gate := step.Action == "assert" || step.Action == "wait_for" || step.Action == "download_wait" || step.Action == "popup_wait" || step.Action == "dialog_wait"
		steps = append(steps, PipelineStep{ID: step.ID, Tool: BrowserToolName, Arguments: arguments, RequirePassed: gate, Bindings: step.Bindings, IfSuccess: step.IfSuccess, IfFailed: step.IfFailed, OnError: step.OnError, When: step.When, RetryOn: step.RetryOn})
		if step.Verify != nil {
			verify := *step.Verify
			verify.Action = "assert"
			verifyBindings := map[string]string{}
			if verify.ExpectedTabID == "" {
				verify.ExpectedTabID = target.ExpectedTabID
				if source := step.Bindings["advanced.expected_tab_id"]; source != "" {
					verifyBindings["advanced.expected_tab_id"] = source
				}
			}
			if verify.ExpectedOrigin == "" {
				verify.ExpectedOrigin = target.ExpectedOrigin
			}
			if verify.FrameID == "" {
				verify.FrameID = target.FrameID
				if source := step.Bindings["advanced.frame_id"]; source != "" {
					verifyBindings["advanced.frame_id"] = source
				}
			}
			if err := browser.ValidateWorkflowRequest(verify); err != nil {
				return nil, fmt.Errorf("step %s verification: %w", step.ID, err)
			}
			arguments, err := browserWorkflowArguments(BrowserWorkflowStep{Action: "assert"}, verify, "")
			if err != nil {
				return nil, err
			}
			steps = append(steps, PipelineStep{ID: step.ID + "/verify", Tool: BrowserToolName, Arguments: arguments, RequirePassed: true, Bindings: verifyBindings, IfSuccess: step.ID})
		}
	}
	return steps, nil
}

func browserWorkflowArguments(step BrowserWorkflowStep, target browser.Request, key string) (map[string]any, error) {
	params := BrowserParams{Action: step.Action, Advanced: target, URL: step.URL, Key: key, Ref: step.Ref, Direction: step.Direction, Amount: step.Amount, Annotate: step.Annotate}
	if step.Action == "type" {
		params.Text = target.Text
	}
	if step.Action == "click" || step.Action == "type" {
		params.Selector = target.Selector
	}
	if step.Action == "key" {
		params.Key = firstNonEmpty(step.Key, target.Key)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 64*1024 {
		return nil, fmt.Errorf("browser workflow step arguments exceed 64 KiB")
	}
	var args map[string]any
	if err := json.Unmarshal(encoded, &args); err != nil {
		return nil, err
	}
	// A struct time encodes even when unset; an unbound newer_than is not
	// a condition and must not reach the browser as year 1.
	if advanced, ok := args["advanced"].(map[string]any); ok && target.NewerThan.IsZero() {
		delete(advanced, "newer_than")
	}
	return args, nil
}
