package tools

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// desktopModes lists every value DesktopWorkflowParams.Mode accepts.
var desktopModes = []string{"observe", "prepare", "act", "fill_submit", "sequence", "adaptive", "transition", "flow", "rename"}

// contractResponse renders a ContractError for every provider: the code-first
// text is what models read, and the same document is attached as metadata for
// UIs, hooks and tests.
func contractResponse(err *computer.ContractError) fantasy.ToolResponse {
	resp := fantasy.NewTextErrorResponse(err.Error())
	if data, marshalErr := json.Marshal(map[string]any{"tool_contract": err}); marshalErr == nil {
		resp.Metadata = string(data)
	}
	return resp
}

// errorResponse converts any validation error, structured or plain, to a tool
// response without losing the contract document.
func errorResponse(err error) fantasy.ToolResponse {
	if contract, ok := computer.AsContractError(err); ok {
		return contractResponse(contract)
	}
	return fantasy.NewTextErrorResponse(err.Error())
}

// misroutedDesktopModes are pipeline modes that models send as computer actions.
var misroutedDesktopModes = []string{"prepare", "act", "fill_submit", "sequence", "adaptive", "transition", "flow", "rename"}

// wrongToolError reports a desktop-recipe mode sent to computer. It is the root
// cause, so it is reported before any field-level error in the same call.
func wrongToolError(normalized, action string) *computer.ContractError {
	if !slices.Contains(misroutedDesktopModes, normalized) {
		return nil
	}
	return computer.NewContractError("wrong_tool", "action", fmt.Sprintf("%q is a tool_pipeline desktop mode, not a computer action. Computer actions: %s. No input sent", action, strings.Join(computerActions, ", ")), "Call tool_pipeline with desktop.mode set to this value; use computer only for single actions such as windows, observe, focus or key.", `{"desktop":{"mode":"prepare","application":"<application name>","observation":"auto"}}`)
}

// unknownComputerAction explains an invalid action. A desktop-recipe mode sent
// to computer gets the exact corrected call shape instead of a bare list.
func unknownComputerAction(action string) fantasy.ToolResponse {
	normalized := strings.ToLower(strings.TrimSpace(action))
	if err := wrongToolError(normalized, action); err != nil {
		return contractResponse(err)
	}
	err := computer.NewContractError("unknown_action", "action", fmt.Sprintf("unknown action %q, must be one of: %s. No input sent", action, strings.Join(computerActions, ", ")), "Choose one listed action.", `{"action":"windows"}`)
	return contractResponse(err)
}

// UnmarshalJSON rejects wrong nesting and unknown fields instead of dropping
// them, so a model that wraps or misplaces a recipe is told exactly how.
func (p *DesktopWorkflowParams) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, nested := fields["desktop"]; nested {
		return computer.NewContractError("invalid_nesting", "desktop.desktop", "desktop contains another desktop object; the recipe fields (mode, input, steps, ...) go directly inside desktop. No input sent", "Remove the inner desktop wrapper.", `{"desktop":{"mode":"act","window_id":"<window_id from prepare/observe>","input":{"action":"key","key":"enter","automation":{"window_id":"<same window_id>"}}}}`)
	}
	if unknown := computer.UnknownFields(fields, DesktopWorkflowParams{}); len(unknown) > 0 {
		hints := map[string]string{
			"action": "a computer action belongs inside input", "automation": "automation belongs inside input",
			"key": "keyboard fields belong inside input", "text": "text belongs inside input", "x": "coordinates belong inside input", "y": "coordinates belong inside input",
			"checkpoint": "checkpoint belongs inside each steps[] entry", "element_id": "selectors belong inside input.automation", "name": "selectors belong inside input.automation",
			"role": "selectors belong inside input.automation", "condition": "conditions belong inside wait_for or a checkpoint", "expected": "expectations belong inside wait_for or a checkpoint",
		}
		var notes []string
		for _, key := range unknown {
			if hint, ok := hints[key]; ok {
				notes = append(notes, key+": "+hint)
			}
		}
		message := fmt.Sprintf("unknown desktop field(s) %s; allowed: %s. Unknown fields are rejected instead of ignored. No input sent", strings.Join(unknown, ", "), strings.Join(computer.KnownFields(DesktopWorkflowParams{}), ", "))
		if len(notes) > 0 {
			message += ". " + strings.Join(notes, "; ")
		}
		return computer.NewContractError("unknown_field", "desktop."+unknown[0], message, "Move or remove the listed fields and call again.", `{"desktop":{"mode":"observe","window_id":"<window_id from prepare/windows>","observation":"semantic"}}`)
	}
	type plain DesktopWorkflowParams
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = DesktopWorkflowParams(decoded)
	return nil
}

// invalidDesktopMode explains a bad mode with the closest correct call shape.
func invalidDesktopMode(mode string) fantasy.ToolResponse {
	message := fmt.Sprintf("desktop mode %q is invalid; mode must be %s. No input sent", mode, strings.Join(desktopModes, "/"))
	next := "Choose one listed mode."
	if slices.Contains(computerActions, strings.ToLower(strings.TrimSpace(mode))) {
		next = "That value is a computer action. Use the computer tool, or mode act with input.action set to it."
	}
	return contractResponse(computer.NewContractError("invalid_mode", "desktop.mode", message, next, `{"desktop":{"mode":"prepare","application":"<application name>","observation":"auto"}}`))
}

// SchemaViolationResponse reports a child call that failed JSON-schema
// validation in the nested dispatcher. The tool's own contract is consulted
// first so a wrong action, mode or nesting gets the same structured,
// correctable answer as a direct call; nothing has been sent when it returns.
func SchemaViolationResponse(call fantasy.ToolCall, violation error) fantasy.ToolResponse {
	switch call.Name {
	case ComputerToolName:
		var p ComputerParams
		if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
			return errorResponse(err)
		}
		if !slices.Contains(computerActions, strings.ToLower(strings.TrimSpace(p.Action))) {
			return unknownComputerAction(p.Action)
		}
	case "tool_pipeline":
		var p PipelineParams
		if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
			return errorResponse(err)
		}
		if p.Desktop != nil && !slices.Contains(desktopModes, p.Desktop.Mode) {
			return invalidDesktopMode(p.Desktop.Mode)
		}
	}
	return contractResponse(computer.NewContractError("invalid_arguments", "", fmt.Sprintf("%s arguments do not match the tool schema: %s. No input sent", call.Name, violation), "Correct the listed fields; the tool description shows the valid shape.", ""))
}
