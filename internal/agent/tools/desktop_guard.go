package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// The desktop guard is the shared execution-layer control for computer and
// tool_pipeline. Every provider and every path (main agent, sub-agent, direct
// call, batch, pipeline children) is wrapped by the same coordinator filter,
// so none of it depends on the model following prompt advice.
//
// It does three things:
//   - classifies each failure (argument, target_state, native, effect_unknown,
//     denied, canceled) so a model error is never mistaken for a native one;
//   - refuses to replay an identical failed call while nothing has changed;
//   - refuses to replay an identical mutation whose effect is unknown until a
//     verified correction replaces it; a read alone never authorizes replay.
//
// It never retries on the model's behalf and never rewrites arguments.

// Failure classes reported in tool metadata.
const (
	failureArgument      = "argument"
	failureTargetState   = "target_state"
	failureNative        = "native"
	failureEffectUnknown = "effect_unknown"
	failureDenied        = "denied"
	failureCanceled      = "canceled"
)

var (
	argumentCodes = map[string]bool{
		"invalid_target": true, "invalid_role": true, "invalid_region": true, "invalid_mode": true, "invalid_nesting": true,
		"invalid_request": true, "invalid_arguments": true, "unknown_field": true, "unknown_action": true, "wrong_tool": true,
		"misplaced_field": true, "conflicting_fields": true, "checkpoint_target_missing": true, "inconsistent_text_checkpoint": true,
		"repeat_blocked": true,
	}
	targetStateCodes = map[string]bool{
		"wrong_window": true, "target_missing": true, "stale_snapshot": true, "stale_element": true, "stale_observation": true,
		"ambiguous_target": true, "unsupported_pattern": true, "target_not_actionable": true, "focus_denied": true,
		"gate_failed": true, "field_not_ready": true, "value_not_applied": true, "condition_timeout": true,
	}
	readOnlyComputerActions = map[string]bool{
		"observe": true, "health": true, "screenshot": true, "screen_size": true, "cursor_position": true, "windows": true,
		"inspect": true, "find": true, "assert": true, "monitors": true, "ocr": true, "capture_region": true, "capture_window": true,
		"status": true, "trace": true,
	}
	effectUnknownText = regexp.MustCompile(`(?i)context canceled|deadline exceeded|timed out|effect_unknown|outcome unknown|interrupted`)
	codePattern       = regexp.MustCompile(`^[a-z][a-z0-9_]{2,40}$`)
)

// desktopRecord remembers one failed call shape.
type desktopRecord struct {
	code     string
	class    string
	failures int
	mutEpoch int
}

type desktopGuardState struct {
	mu       sync.Mutex
	records  map[string]*desktopRecord
	order    []string
	mutEpoch int // Successful state-changing desktop calls.
	// lastFailure is when a desktop call last failed with no success since.
	lastFailure time.Time
	windows     map[string]time.Time
	guiOnly     bool
}

var desktopGuards = struct {
	sync.Mutex
	sessions map[string]*desktopGuardState
	order    []string
}{sessions: map[string]*desktopGuardState{}}

func desktopGuardFor(session string) *desktopGuardState {
	desktopGuards.Lock()
	defer desktopGuards.Unlock()
	state, ok := desktopGuards.sessions[session]
	if ok {
		return state
	}
	if len(desktopGuards.order) >= 64 {
		delete(desktopGuards.sessions, desktopGuards.order[0])
		desktopGuards.order = desktopGuards.order[1:]
	}
	state = &desktopGuardState{records: map[string]*desktopRecord{}, windows: map[string]time.Time{}}
	desktopGuards.sessions[session] = state
	desktopGuards.order = append(desktopGuards.order, session)
	return state
}

type desktopGuardedTool struct {
	fantasy.AgentTool
}

// WithDesktopGuard wraps computer and tool_pipeline with the shared guard.
// Other tools are returned unchanged, except bash which gets the fallback
// notice below.
func WithDesktopGuard(tool fantasy.AgentTool) fantasy.AgentTool {
	switch tool.Info().Name {
	case ComputerToolName, "tool_pipeline":
		return &desktopGuardedTool{AgentTool: tool}
	case BashToolName:
		return &shellFallbackTool{AgentTool: tool}
	case EditToolName, WriteToolName, MultiEditToolName, "apply_patch", "execution":
		return &desktopScopeTool{AgentTool: tool}
	}
	return tool
}

// desktopCallShape returns a canonical key for a call and whether it only reads.
func desktopCallShape(call fantasy.ToolCall) (key string, readOnly bool) {
	var generic any
	canonical := call.Input
	if call.Name == ComputerToolName {
		var params ComputerParams
		if json.Unmarshal([]byte(call.Input), &params) == nil {
			params.Action = strings.ToLower(strings.TrimSpace(params.Action))
			if data, err := json.Marshal(params); err == nil {
				canonical = string(data)
			}
		}
	}
	if json.Unmarshal([]byte(canonical), &generic) == nil {
		if data, err := json.Marshal(generic); err == nil {
			canonical = string(data)
		}
	}
	sum := sha256.Sum256([]byte(call.Name + "\x00" + canonical))
	key = hex.EncodeToString(sum[:])
	switch call.Name {
	case ComputerToolName:
		var p struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal([]byte(call.Input), &p)
		readOnly = readOnlyComputerActions[strings.ToLower(strings.TrimSpace(p.Action))]
	case "tool_pipeline":
		var p struct {
			Desktop *struct {
				Mode string `json:"mode"`
			} `json:"desktop"`
		}
		readOnly = json.Unmarshal([]byte(call.Input), &p) == nil && p.Desktop != nil && p.Desktop.Mode == "observe"
	}
	return key, readOnly
}

func responseCode(resp fantasy.ToolResponse) string {
	code, _, _ := strings.Cut(strings.TrimSpace(resp.Content), ":")
	if codePattern.MatchString(code) {
		return code
	}
	if strings.HasPrefix(resp.Content, "invalid parameters") {
		return "invalid_arguments"
	}
	return "error"
}

func classifyFailure(ctx context.Context, resp fantasy.ToolResponse, err error, readOnly bool) (code, class string) {
	switch {
	case err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)), ctx.Err() != nil:
		code, class = "canceled", failureCanceled
		if !readOnly {
			class = failureEffectUnknown
		}
		return code, class
	case err != nil:
		if !readOnly && effectUnknownText.MatchString(err.Error()) {
			return "tool_error", failureEffectUnknown
		}
		return "tool_error", failureNative
	}
	code = responseCode(resp)
	var metadata struct {
		Contract *computer.ContractError `json:"tool_contract"`
	}
	_ = json.Unmarshal([]byte(resp.Metadata), &metadata)
	switch {
	case resp.StopTurn && strings.Contains(resp.Content, "denied"):
		return "permission_denied", failureDenied
	case strings.HasPrefix(resp.Content, "Tool call blocked by hook"), strings.HasPrefix(resp.Content, "Turn halted by hook"):
		return "hook_denied", failureDenied
	case argumentCodes[code]:
		return code, failureArgument
	case !readOnly && metadata.Contract != nil && metadata.Contract.Effect == "unknown":
		return code, failureEffectUnknown
	case !readOnly && code == "condition_timeout" && (metadata.Contract == nil || metadata.Contract.Effect != "none"):
		return code, failureEffectUnknown
	case !readOnly && effectUnknownText.MatchString(resp.Content):
		return code, failureEffectUnknown
	case targetStateCodes[code]:
		return code, failureTargetState
	}
	return code, failureNative
}

func (t *desktopGuardedTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	state := desktopGuardFor(GetSessionFromContext(ctx))
	key, readOnly := desktopCallShape(call)

	state.mu.Lock()
	rec := state.records[key]
	var blocked *computer.ContractError
	if rec != nil {
		blocked = repeatBlock(rec, state, readOnly)
	}
	state.mu.Unlock()
	if blocked != nil {
		return contractResponse(blocked), nil
	}
	if call.Name == ComputerToolName {
		var p ComputerParams
		if json.Unmarshal([]byte(call.Input), &p) == nil {
			if err := requireObservedDesktopTarget(ctx, p); err != nil {
				return contractResponse(err), nil
			}
		}
	}
	ctx = context.WithValue(ctx, desktopGuardContextKey{}, true)

	resp, err := t.AgentTool.Run(ctx, call)
	if call.Name == ComputerToolName && err == nil && !resp.IsError && !resp.StopTurn && ctx.Err() == nil {
		var p ComputerParams
		if json.Unmarshal([]byte(call.Input), &p) == nil {
			recordDesktopWindows(ctx, p.Action, resp)
		}
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if err == nil && !resp.IsError && !resp.StopTurn && ctx.Err() == nil {
		delete(state.records, key)
		state.lastFailure = time.Time{}
		if !readOnly {
			state.mutEpoch++
		}
		return resp, nil
	}
	code, class := classifyFailure(ctx, resp, err, readOnly)
	state.lastFailure = time.Now()
	if rec = state.records[key]; rec == nil {
		if len(state.order) >= 64 {
			delete(state.records, state.order[0])
			state.order = state.order[1:]
		}
		rec = &desktopRecord{}
		state.records[key] = rec
		state.order = append(state.order, key)
	}
	rec.code, rec.class = code, class
	rec.failures++
	rec.mutEpoch = state.mutEpoch
	if err == nil {
		resp.Metadata = withFailureClass(resp.Metadata, code, class)
	}
	return resp, err
}

// repeatBlock decides whether an identical call may run again. The caller
// holds the state lock.
func repeatBlock(rec *desktopRecord, state *desktopGuardState, readOnly bool) *computer.ContractError {
	var why, next string
	switch rec.class {
	case failureArgument:
		why = "the same arguments were already rejected as invalid, and re-sending them cannot succeed"
		next = "Change the arguments according to the earlier error before calling again."
	case failureDenied:
		why = "this call was denied; it is not retried without a new decision"
		next = "Do not retry. Ask the user, or choose a different permitted approach."
	case failureEffectUnknown:
		why = "the earlier attempt may or may not have taken effect (it was interrupted or timed out), and replaying an input could apply it twice"
		next = "Do not replay this call, even after a read. Inspect the intended target and verify the outcome. If correction is needed, use a distinct operation with a verified postcondition (for example set_value for an editable field); otherwise hand off."
	case failureCanceled:
		return nil
	default:
		if state.mutEpoch > rec.mutEpoch || (readOnly && rec.failures < 2) {
			return nil
		}
		why = "the same call already failed and no successful action has changed the desktop state since"
		next = "Observing alone does not change the state. Resolve the cause (for example focus the intended window), change the arguments, or report the blocker."
	}
	err := computer.NewContractError("repeat_blocked", "", fmt.Sprintf("identical call refused: %s (previous result: %s). No input sent", why, rec.code), next, "")
	err.FreshObservation = rec.class != failureArgument && rec.class != failureDenied
	return err
}

// withFailureClass records the failure class next to any existing metadata.
func withFailureClass(existing, code, class string) string {
	metadata := map[string]any{}
	if existing != "" {
		if json.Unmarshal([]byte(existing), &metadata) != nil || metadata == nil {
			metadata = map[string]any{"original_metadata": existing}
		}
	}
	metadata["desktop_failure"] = map[string]string{"code": code, "class": class}
	data, err := json.Marshal(metadata)
	if err != nil {
		return existing
	}
	return string(data)
}

// shellFallbackTool makes a GUI-to-shell substitution visible. When a desktop
// call failed and nothing desktop-side has succeeded since, a state-changing
// shell command is not a GUI result: the response says so and is flagged, so a
// task that required GUI steps is not silently reported as completed by them.
type shellFallbackTool struct {
	fantasy.AgentTool
}

var shellMutation = regexp.MustCompile(`(?i)\b(mkdir|md|new-item|ni|set-content|add-content|out-file|remove-item|ri|rm|del|erase|rename-item|rni|ren|move-item|mv|move|copy-item|cp|copy|touch|tee|start-process|taskkill|stop-process)\b|[^<>|]>>?[^&]`)

func (t *shellFallbackTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if resp, blocked := desktopScopeViolation(ctx, call.Name); blocked {
		return resp, nil
	}
	resp, err := t.AgentTool.Run(ctx, call)
	if err != nil {
		return resp, err
	}
	state := desktopGuardFor(GetSessionFromContext(ctx))
	state.mu.Lock()
	recovering := !state.lastFailure.IsZero() && time.Since(state.lastFailure) < 10*time.Minute
	state.mu.Unlock()
	if !recovering {
		return resp, nil
	}
	var params struct {
		Command string   `json:"command"`
		Argv    []string `json:"argv"`
	}
	if json.Unmarshal([]byte(call.Input), &params) != nil {
		return resp, nil
	}
	if !shellMutation.MatchString(params.Command + " " + strings.Join(params.Argv, " ")) {
		return resp, nil
	}
	resp.Content += "\n\nGUI fallback notice: a desktop step failed earlier and this command changes state through the shell. It does not complete any GUI step. If the task required doing this in the application, do not report the GUI step as done; tell the user it was done through the shell, or continue with the GUI."
	resp.Metadata = mergeFallbackMetadata(resp.Metadata)
	return resp, nil
}

func mergeFallbackMetadata(existing string) string {
	metadata := map[string]any{}
	if existing != "" {
		if json.Unmarshal([]byte(existing), &metadata) != nil || metadata == nil {
			return existing
		}
	}
	metadata["gui_fallback"] = true
	data, err := json.Marshal(metadata)
	if err != nil {
		return existing
	}
	return string(data)
}
