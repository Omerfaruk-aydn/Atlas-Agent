package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

var pipelineErrorCodePattern = regexp.MustCompile(`"error_code":"([a-z_]+)"`)

var (
	pipelineDispatchPattern   = regexp.MustCompile(`"dispatch"\s*:\s*"(none|sent|partial|unknown)"`)
	pipelineActionSentPattern = regexp.MustCompile(`"action_sent"\s*:\s*true`)
)

// pipelineDispatch preserves the strongest dispatch evidence across content
// and metadata, including partial operations whose later input was refused.
func pipelineDispatch(texts ...string) string {
	dispatch := ""
	for _, text := range texts {
		for _, match := range pipelineDispatchPattern.FindAllStringSubmatch(text, -1) {
			switch match[1] {
			case "unknown":
				return "unknown"
			case "sent", "partial":
				dispatch = "sent"
			case "none":
				if dispatch == "" {
					dispatch = "none"
				}
			}
		}
		if pipelineActionSentPattern.MatchString(text) {
			dispatch = "sent"
		}
	}
	return dispatch
}

// pipelineReadOnlyRecovery allows inspection after an unknown effect.
// Opaque tools, including MCP and shell calls, cannot establish read-only
// behavior from their names or caller-supplied arguments.
func pipelineReadOnlyRecovery(s PipelineStep) bool {
	switch s.Tool {
	case ViewToolName, GrepToolName, GlobToolName, LSToolName:
		return true
	case BrowserToolName:
		switch s.Arguments["action"] {
		case "observe", "screenshot", "snapshot", "text", "html", "url", "tabs", "frames", "find", "assert", "wait_for", "read_field", "collect", "download_wait", "popup_wait", "dialog_wait":
			return true
		}
	case ComputerToolName:
		switch s.Arguments["action"] {
		case "observe", "screenshot", "screen_size", "cursor_position", "windows", "inspect", "find", "assert", "monitors", "ocr", "capture_region", "capture_window":
			return true
		}
	}
	return false
}

// pipelineErrorCode is the error class of a failed step: the code its op
// record or metadata carries, else the shared browser classification.
func pipelineErrorCode(resp fantasy.ToolResponse) string {
	if !resp.IsError {
		return ""
	}
	for _, text := range []string{resp.Content, resp.Metadata} {
		if m := pipelineErrorCodePattern.FindStringSubmatch(text); m != nil {
			return m[1]
		}
	}
	code, _ := browserErrorCode(resp.Content)
	return code
}

// pipelineMayRetry allows the single retry a step asked for: a browser step
// with a semantic target whose failure is in its retry_on list, was refused
// before any input, and is not a gate.
func pipelineMayRetry(s PipelineStep, code, content string, metadata ...string) bool {
	if s.Tool != BrowserToolName || len(s.RetryOn) == 0 || !pipelineRetryCodes[code] {
		return false
	}
	listed := false
	for _, c := range s.RetryOn {
		listed = listed || c == code
	}
	dispatch := pipelineDispatch(append([]string{content}, metadata...)...)
	if !listed || dispatch == "sent" || dispatch == "partial" || dispatch == "unknown" {
		return false
	}
	return pipelineSemanticTarget(s)
}

// pipelineSemanticTarget reports a browser step that names its target by
// role/name/label: a fresh observation can resolve it again. A ref or
// selector would point at the same, possibly reused, node.
func pipelineSemanticTarget(s PipelineStep) bool {
	if s.Arguments["ref"] != nil || s.Arguments["selector"] != nil || s.Bindings["ref"] != "" {
		return false
	}
	advanced, _ := s.Arguments["advanced"].(map[string]any)
	if advanced == nil || advanced["selector"] != nil {
		return false
	}
	return advanced["role"] != nil || advanced["name"] != nil || advanced["label"] != nil
}

func validatePipelineRetry(s PipelineStep) string {
	if len(s.RetryOn) == 0 {
		return ""
	}
	if s.Tool != BrowserToolName || len(s.Items) > 0 {
		return fmt.Sprintf("step %s: retry_on applies to single browser steps", s.ID)
	}
	for _, code := range s.RetryOn {
		if !pipelineRetryCodes[code] {
			return fmt.Sprintf("step %s: retry_on supports target_not_found, target_resolution_failed, condition_timeout and stale_ref; %q may have sent input or cannot change by observing", s.ID, code)
		}
	}
	if !pipelineSemanticTarget(s) {
		return fmt.Sprintf("step %s: retry_on needs a semantic target (advanced role/name/label); a ref or selector would aim at the same node again", s.ID)
	}
	return ""
}

func validatePipelineCondition(s PipelineStep, seen map[string]bool) string {
	c := s.When
	if c == nil {
		return ""
	}
	id, _, ok := strings.Cut(c.Source, "#/")
	if !ok || !seen[id] {
		return fmt.Sprintf("step %s: when.source must be an earlier step#/field", s.ID)
	}
	given := 0
	if c.Equals != nil {
		given++
	}
	if c.NotEquals != nil {
		given++
	}
	if c.Exists != nil {
		given++
	}
	if given != 1 {
		return fmt.Sprintf("step %s: when needs exactly one of equals, not_equals or exists", s.ID)
	}
	return ""
}

// pipelineConditionMet evaluates when against earlier results. A source
// step that failed or was skipped has no result: only exists:false runs.
func pipelineConditionMet(c PipelineCondition, outputs map[string]json.RawMessage) (bool, string) {
	value, err := pipelinePointer(outputs, c.Source)
	if c.Exists != nil {
		if (err == nil) == *c.Exists {
			return true, ""
		}
		return false, fmt.Sprintf("when %s exists=%v is not met", c.Source, *c.Exists)
	}
	if err != nil {
		return false, "when " + c.Source + " is unreadable: " + err.Error()
	}
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case float64:
		text = strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		text = strconv.FormatBool(v)
	default:
		return false, "when " + c.Source + " is not a scalar"
	}
	if c.Equals != nil && text != *c.Equals {
		return false, fmt.Sprintf("when %s is %q, not %q", c.Source, text, *c.Equals)
	}
	if c.NotEquals != nil && text == *c.NotEquals {
		return false, fmt.Sprintf("when %s is %q", c.Source, text)
	}
	return true, ""
}
