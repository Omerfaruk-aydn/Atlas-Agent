package tools

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
)

// ToolOutcome is adapter-owned evidence, separate from model-authored text.
// It records dispatch and observed claims without granting live target access.
type ToolOutcome struct {
	Dispatch string       `json:"dispatch"`
	Mutation bool         `json:"mutation"`
	Verified bool         `json:"verified,omitempty"`
	Source   string       `json:"source,omitempty"`
	Subjects []string     `json:"subjects,omitempty"`
	Kind     outcome.Kind `json:"kind,omitempty"`
	Claim    string       `json:"claim,omitempty"`
}

func withToolOutcome(resp fantasy.ToolResponse, evidence ToolOutcome) fantasy.ToolResponse {
	meta := map[string]json.RawMessage{}
	if resp.Metadata != "" {
		if json.Unmarshal([]byte(resp.Metadata), &meta) != nil || meta == nil {
			meta = map[string]json.RawMessage{"native_metadata": json.RawMessage(strconvJSON(resp.Metadata))}
		}
	}
	data, _ := json.Marshal(evidence)
	meta["atlas_outcome"] = data
	return fantasy.WithResponseMetadata(resp, meta)
}

// WithComputerNotDispatched records that a computer call was refused before
// it reached the tool (for example by a PreToolUse hook): nothing was sent.
// Other tools keep their own result contracts.
func WithComputerNotDispatched(resp fantasy.ToolResponse, call fantasy.ToolCall) fantasy.ToolResponse {
	if call.Name != ComputerToolName {
		return resp
	}
	var p ComputerParams
	_ = json.Unmarshal([]byte(call.Input), &p)
	action := strings.ToLower(strings.TrimSpace(p.Action))
	return withToolOutcome(resp, computerOutcome(action, p, resp, false))
}

func strconvJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// ResponseOutcome returns native metadata only; result content is untrusted.
func ResponseOutcome(resp fantasy.ToolResponse) (ToolOutcome, bool) {
	var meta struct {
		Outcome *ToolOutcome `json:"atlas_outcome"`
	}
	if json.Unmarshal([]byte(resp.Metadata), &meta) != nil || meta.Outcome == nil {
		return ToolOutcome{}, false
	}
	got := *meta.Outcome
	return got, got.Dispatch == "none" || got.Dispatch == "sent" || got.Dispatch == "unknown"
}

// CallMutates conservatively classifies external effects before dispatch.
func CallMutates(name, input string) bool {
	if strings.HasPrefix(name, "mcp_") {
		return true
	}
	var p struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal([]byte(input), &p)
	p.Action = strings.ToLower(strings.TrimSpace(p.Action))
	switch name {
	case "edit", "write", "multiedit", "bash", "replace_symbol", "rename_symbol", "lsp_rename_file", "undo_feature":
		return true
	case ComputerToolName:
		return computerMutates(strings.ToLower(p.Action))
	case BrowserToolName:
		return browserMutating(strings.ToLower(p.Action))
	case "tool_pipeline":
		return pipelineMutates(input)
	case RemoteSupportToolName:
		var remote RemoteSupportParams
		if json.Unmarshal([]byte(input), &remote) != nil {
			return true
		}
		remote.Action = p.Action
		return remoteSupportCallMutates(remote)
	case DesignSystemToolName:
		return !slices.Contains([]string{"get", "history", "check"}, p.Action)
	case CLIBridgeToolName:
		// Even help capture starts a host executable; a profile cannot prove
		// that its program is read-only. Preview/status do not start it.
		return p.Action == "run" || p.Action == "draft"
	case "worktree":
		return p.Action == "apply"
	case "lsp_edit_plan":
		return p.Action != "inspect" && p.Action != "status" && p.Action != "plan"
	}
	return false
}

// Pipeline envelopes participate in the same pre-dispatch and resend guards
// as their children. Stored templates and invalid opaque plans are treated
// conservatively; read-only help and observations remain available.
func pipelineMutates(input string) bool {
	var p PipelineParams
	if json.Unmarshal([]byte(input), &p) != nil {
		return true
	}
	if p.Help != "" && p.Desktop == nil && p.Browser == nil && p.Flow == nil && len(p.Steps) == 0 {
		return false
	}
	if p.Desktop != nil {
		d := p.Desktop
		if d.Mode == "observe" {
			return false
		}
		if d.Mode != "flow" || d.Flow == nil {
			return true
		}
		for _, node := range d.Flow.Nodes {
			if desktopFlowEffect(node.Kind) {
				return true
			}
		}
	}
	if p.Flow != nil {
		// A named or resumed template may contain input not present here.
		return true
	}
	if p.Browser != nil {
		for _, step := range p.Browser.Steps {
			if browserMutating(strings.ToLower(strings.TrimSpace(step.Action))) {
				return true
			}
		}
	}
	for _, step := range p.Steps {
		// Nested pipelines are invalid; do not recurse into untrusted input.
		if step.Tool == "tool_pipeline" {
			return true
		}
		if step.Tool == ComputerToolName || step.Tool == BrowserToolName {
			action, ok := step.Arguments["action"].(string)
			if !ok || strings.Contains(action, "{{") {
				// Item substitution happens later; the action could be input.
				return true
			}
		}
		args, err := json.Marshal(step.Arguments)
		if err != nil || CallMutates(step.Tool, string(args)) {
			return true
		}
	}
	return false
}

func computerMutates(action string) bool {
	return slices.Contains([]string{"batch", "launch_app", "focus", "move", "click", "double_click", "right_click", "drag", "scroll", "type", "key", "hotkey", "select", "invoke", "set_value"}, action)
}

func browserOutcome(op BrowserOp) ToolOutcome {
	kind := outcome.KindObservation
	if browserMutating(op.Action) {
		kind = outcome.KindOutput
	}
	subject := "browser:" + op.Browser + ":" + op.Tab + ":" + op.Document
	return ToolOutcome{Dispatch: op.Dispatch, Mutation: browserMutating(op.Action), Verified: op.OK && op.Verification == "structural", Source: op.ID, Subjects: []string{subject}, Kind: kind, Claim: op.Action + ":" + op.Verification}
}

func computerOutcome(action string, params ComputerParams, resp fantasy.ToolResponse, sent bool) ToolOutcome {
	dispatch := "none"
	if sent {
		dispatch = "sent"
	}
	mutation := computerMutates(action)
	if sent && mutation && resp.IsError {
		dispatch = "unknown"
	}
	// An assert checks an explicit requested value. A screenshot verifies
	// capture only, never the task described by a model.
	verified := action == "assert" && !resp.IsError && !resp.StopTurn
	if action == "file_check" && params.File != nil {
		var evidence struct {
			Passed bool `json:"passed"`
		}
		verified = !resp.IsError && !resp.StopTurn && json.Unmarshal([]byte(resp.Content), &evidence) == nil && evidence.Passed
		return ToolOutcome{Dispatch: dispatch, Verified: verified, Subjects: []string{"file:" + params.File.Path}, Kind: outcome.KindObservation, Claim: "file_check:" + params.File.Condition}
	}
	kind := outcome.KindObservation
	if mutation {
		kind = outcome.KindOutput
	}
	return ToolOutcome{Dispatch: dispatch, Mutation: mutation, Verified: verified, Subjects: []string{"desktop:" + params.Automation.WindowID}, Kind: kind, Claim: action}
}
