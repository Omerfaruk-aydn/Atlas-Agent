package hooks

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/tidwall/gjson"
)

// SupportedOutputVersion is the highest envelope version this build
// understands. Hooks may omit `version` entirely (treated as 1) or pin
// an older version. Unknown higher versions are still parsed but logged.
const SupportedOutputVersion = 1

// Payload is the JSON structure piped to hook commands via stdin.
// ToolInput is emitted as a parsed JSON object for compatibility with
// Claude Code hooks (which expect tool_input to be an object, not a
// string).
type Payload struct {
	Event     string          `json:"event"`
	SessionID string          `json:"session_id"`
	CWD       string          `json:"cwd"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	// ToolResponse is what the tool returned. Only PostToolUse hooks see
	// it, and it is truncated to MaxToolResponseInPayload: a hook reads
	// this off a pipe, and a tool that returned megabytes should not
	// stall the turn feeding them to a shell script.
	ToolResponse string `json:"tool_response,omitempty"`
	// Prompt is what the user submitted. Only UserPromptSubmit hooks see it.
	Prompt string `json:"prompt,omitempty"`
}

// MaxToolResponseInPayload caps how much of a tool's output is handed to a
// PostToolUse hook.
const MaxToolResponseInPayload = 10_000

// BuildPayload constructs the JSON stdin payload for a hook command.
func BuildPayload(eventName, sessionID, cwd, toolName, toolInputJSON string) []byte {
	return BuildPayloadWithResponse(eventName, sessionID, cwd, toolName, toolInputJSON, "")
}

// BuildPayloadWithResponse is BuildPayload plus the tool's output, for
// PostToolUse hooks.
func BuildPayloadWithResponse(eventName, sessionID, cwd, toolName, toolInputJSON, toolResponse string) []byte {
	return buildPayload(runInput{
		eventName:     eventName,
		sessionID:     sessionID,
		toolName:      toolName,
		toolInputJSON: toolInputJSON,
		toolResponse:  toolResponse,
	}, cwd)
}

func buildPayload(in runInput, cwd string) []byte {
	toolInput := json.RawMessage(in.toolInputJSON)
	if !json.Valid(toolInput) {
		toolInput = json.RawMessage("{}")
	}
	toolResponse := in.toolResponse
	if len(toolResponse) > MaxToolResponseInPayload {
		toolResponse = toolResponse[:MaxToolResponseInPayload] + "\n... [truncated]"
	}
	p := Payload{
		Event:        in.eventName,
		SessionID:    in.sessionID,
		CWD:          cwd,
		ToolName:     in.toolName,
		ToolInput:    toolInput,
		ToolResponse: toolResponse,
		Prompt:       in.prompt,
	}
	data, err := json.Marshal(p)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// BuildEnv constructs the environment variable slice for a hook command.
// It includes all current process env vars plus hook-specific ones.
func BuildEnv(eventName, toolName, sessionID, cwd, projectDir, toolInputJSON string) []string {
	env := os.Environ()
	env = append(env, shell.AtlasAgentEnvMarkers()...)
	env = append(
		env,
		fmt.Sprintf("ATLAS_AGENT_EVENT=%s", eventName),
		fmt.Sprintf("ATLAS_AGENT_TOOL_NAME=%s", toolName),
		fmt.Sprintf("ATLAS_AGENT_SESSION_ID=%s", sessionID),
		fmt.Sprintf("ATLAS_AGENT_CWD=%s", cwd),
		fmt.Sprintf("ATLAS_AGENT_PROJECT_DIR=%s", projectDir),
	)
	env = append(env, fmt.Sprintf("ATLAS_AGENT_PROMPT=%s", ""))
	// Extract tool-specific env vars from the JSON input.
	if toolInputJSON != "" {
		if cmd := gjson.Get(toolInputJSON, "command"); cmd.Exists() {
			env = append(env, fmt.Sprintf("ATLAS_AGENT_TOOL_INPUT_COMMAND=%s", cmd.String()))
		}
		if fp := gjson.Get(toolInputJSON, "file_path"); fp.Exists() {
			env = append(env, fmt.Sprintf("ATLAS_AGENT_TOOL_INPUT_FILE_PATH=%s", fp.String()))
		}
	}

	return env
}

// parseStdout parses the JSON output from a hook command's stdout.
// Supports both Atlas-Agent format and Claude Code format (hookSpecificOutput).
func parseStdout(stdout string) HookResult {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return HookResult{Decision: DecisionNone}
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return HookResult{Decision: DecisionNone}
	}

	// Claude Code compat: if hookSpecificOutput is present, parse that.
	if hso, ok := raw["hookSpecificOutput"]; ok {
		return parseClaudeCodeOutput(hso)
	}

	var parsed struct {
		Version      int             `json:"version"`
		Decision     string          `json:"decision"`
		Halt         bool            `json:"halt"`
		Reason       string          `json:"reason"`
		Context      json.RawMessage `json:"context"`
		UpdatedInput json.RawMessage `json:"updated_input"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return HookResult{Decision: DecisionNone}
	}

	if parsed.Version > SupportedOutputVersion {
		slog.Debug(
			"Hook output declared a newer envelope version than this build supports",
			"version", parsed.Version,
			"supported", SupportedOutputVersion,
		)
	}

	result := HookResult{
		Halt:    parsed.Halt,
		Reason:  parsed.Reason,
		Context: parseContext(parsed.Context),
	}
	result.Decision = parseDecision(parsed.Decision)
	result.UpdatedInput = rawToString(parsed.UpdatedInput)
	return result
}

// parseContext accepts either a single string or an array of strings and
// returns a newline-joined value with empty entries dropped.
func parseContext(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// String form.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}
	// Array form.
	if raw[0] == '[' {
		var items []string
		if err := json.Unmarshal(raw, &items); err != nil {
			return ""
		}
		out := items[:0]
		for _, s := range items {
			if s != "" {
				out = append(out, s)
			}
		}
		return strings.Join(out, "\n")
	}
	return ""
}

// parseClaudeCodeOutput handles the Claude Code hook output format:
// {"hookSpecificOutput": {"permissionDecision": "allow", ...}}
func parseClaudeCodeOutput(data json.RawMessage) HookResult {
	var hso struct {
		PermissionDecision       string          `json:"permissionDecision"`
		PermissionDecisionReason string          `json:"permissionDecisionReason"`
		UpdatedInput             json.RawMessage `json:"updatedInput"`
		AdditionalContext        string          `json:"additionalContext"`
	}
	if err := json.Unmarshal(data, &hso); err != nil {
		return HookResult{Decision: DecisionNone}
	}

	result := HookResult{
		Decision: parseDecision(hso.PermissionDecision),
		Reason:   hso.PermissionDecisionReason,
		Context:  hso.AdditionalContext,
	}

	// Marshal updatedInput back to a string for our opaque format.
	if len(hso.UpdatedInput) > 0 && string(hso.UpdatedInput) != "null" {
		result.UpdatedInput = string(hso.UpdatedInput)
	}

	return result
}

// rawToString converts a json.RawMessage to a string suitable for use
// as opaque tool input. It accepts both a JSON object (nested) and a
// JSON string (stringified, for backward compatibility).
func rawToString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// If it's a JSON string, unwrap it.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}
	// Otherwise it's an object/array — use as-is.
	return string(raw)
}

func parseDecision(s string) Decision {
	switch strings.ToLower(s) {
	case "allow":
		return DecisionAllow
	case "deny":
		return DecisionDeny
	default:
		return DecisionNone
	}
}
