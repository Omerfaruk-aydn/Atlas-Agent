package tools

import (
	"encoding/json"
	"fmt"
	"net/url"
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
	Bindings map[string]string `json:"bindings,omitempty" description:"Bind verified earlier JSON outputs, e.g. advanced.tab_id: popup#/tab_id or advanced.newer_than: download#/started_at."`
	ID       string            `json:"id"`
	Action   string            `json:"action"`
	Target   browser.Request   `json:"target,omitempty"`
	URL      string            `json:"url,omitempty"`
	Text     string            `json:"text,omitempty"`
	Key      string            `json:"key,omitempty"`
	Verify   *browser.Request  `json:"verify,omitempty" description:"Optional DOM postcondition; failure stops subsequent steps without replaying the action."`
}

func compileBrowserWorkflow(p BrowserWorkflowParams) ([]PipelineStep, error) {
	if len(p.Steps) < 1 || len(p.Steps) > 32 {
		return nil, fmt.Errorf("browser workflow requires 1-32 steps (including verification: maximum 64 calls)")
	}
	seen := map[string]bool{}
	var steps []PipelineStep
	for _, step := range p.Steps {
		if step.ID == "" || seen[step.ID] || seen[step.ID+"/verify"] {
			return nil, fmt.Errorf("invalid or duplicate browser step id")
		}
		seen[step.ID], seen[step.ID+"/verify"] = true, true
		switch step.Action {
		case "navigate", "semantic_click", "semantic_type", "find", "assert", "wait_for", "tabs", "tab_new", "tab_select", "tab_close", "frames", "upload", "download_start", "download_wait", "popup_wait", "dialog_wait", "dialog_handle", "key":
		default:
			return nil, fmt.Errorf("unsupported browser workflow action %q", step.Action)
		}
		if step.Action == "navigate" {
			u, err := url.Parse(step.URL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return nil, fmt.Errorf("navigate requires an HTTP(S) URL")
			}
		}
		if step.Action == "key" {
			if _, ok := browser.ResolveKey(step.Key); !ok {
				return nil, fmt.Errorf("unsupported workflow key")
			}
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
		validation := target
		if step.Bindings["advanced.tab_id"] != "" {
			validation.TabID = "bound"
		}
		if step.Bindings["advanced.newer_than"] != "" {
			validation.NewerThan = time.Unix(1, 0)
		}
		if err := browser.ValidateWorkflowRequest(validation); err != nil {
			return nil, fmt.Errorf("step %s: %w", step.ID, err)
		}
		arguments, err := browserWorkflowArguments(step.Action, target, step.URL, step.Key)
		if err != nil {
			return nil, err
		}
		gate := step.Action == "assert" || step.Action == "wait_for" || step.Action == "download_wait" || step.Action == "popup_wait" || step.Action == "dialog_wait"
		steps = append(steps, PipelineStep{ID: step.ID, Tool: BrowserToolName, Arguments: arguments, RequirePassed: gate, Bindings: step.Bindings})
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
			arguments, err := browserWorkflowArguments("assert", verify, "", "")
			if err != nil {
				return nil, err
			}
			steps = append(steps, PipelineStep{ID: step.ID + "/verify", Tool: BrowserToolName, Arguments: arguments, RequirePassed: true, Bindings: verifyBindings})
		}
	}
	return steps, nil
}

func browserWorkflowArguments(action string, target browser.Request, url, key string) (map[string]any, error) {
	params := BrowserParams{Action: action, Advanced: target, URL: url, Key: key}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 64*1024 {
		return nil, fmt.Errorf("browser workflow step arguments exceed 64 KiB")
	}
	var args map[string]any
	err = json.Unmarshal(encoded, &args)
	return args, err
}
