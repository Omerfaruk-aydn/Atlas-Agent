// Package hooks runs user-defined shell commands that fire on hook events
// (e.g. PreToolUse), returning decisions that control agent behavior.
package hooks

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/tidwall/sjson"
)

// Hook event name constants.
const (
	EventPreToolUse = "PreToolUse"
	// EventPostToolUse fires after a tool has run, with the tool's output
	// in the payload. It cannot stop the call -- it already happened --
	// but it can halt the turn, and its context is appended to what the
	// model sees.
	EventPostToolUse = "PostToolUse"
	// EventUserPromptSubmit fires before a prompt reaches the model, with
	// the prompt in the payload. It can refuse the prompt outright, or
	// add context the model sees alongside it. Matchers do not apply:
	// there is no tool name to match against.
	EventUserPromptSubmit = "UserPromptSubmit"
	// EventSessionStart fires once per session, the first time this
	// process runs a turn for it -- a genuinely new session and a
	// resumed one both fire it, once each, the first time this process
	// touches them. It cannot refuse the turn; its context is appended to
	// the first prompt the model sees. Matchers do not apply.
	EventSessionStart = "SessionStart"
	// EventSessionEnd fires when a session is explicitly deleted (`atlas
	// session delete`, or a matching prune). It cannot block the
	// deletion -- by the time it fires the session is already gone -- and
	// is meant for cleanup or archival, not for approval. This is
	// narrower than a full session-lifecycle "close": a CLI/TUI turn has
	// no other single, well-defined point at which a session is done, so
	// explicit deletion is the one this fires on. Matchers do not apply.
	EventSessionEnd = "SessionEnd"
	// EventPreCompact fires before auto-summarization runs: a turn that
	// tripped the context-window threshold mid-turn. Unlike EventSessionEnd,
	// it CAN block: a deny or halt decision skips summarization for this
	// turn, and the turn continues over budget rather than losing history
	// the hook did not approve dropping. Matchers do not apply.
	//
	// `atlas session compact` (an explicit, out-of-turn user action, not a
	// silent threshold trip) does not go through this gate -- it calls
	// Summarize directly.
	EventPreCompact = "PreCompact"
)

// HaltExitCode is the exit code that halts the whole turn. 2 blocks the
// current tool call; 49 sits in the no-man's-land between the
// generic-error range (1-30), the sysexits range (64-78), and the
// killed-by-signal range (128+) so it can't be hit by accident.
const HaltExitCode = 49

// HookMetadata is embedded in tool response metadata so the UI can
// display a hook indicator.
type HookMetadata struct {
	HookCount       int        `json:"hook_count"`
	Decision        string     `json:"decision"`
	Halt            bool       `json:"halt,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	InputRewrite    bool       `json:"input_rewrite,omitempty"`
	RequiredFailure bool       `json:"required_failure,omitempty"`
	Hooks           []HookInfo `json:"hooks,omitempty"`
}

// HookInfo identifies a single hook that ran and its individual result.
type HookInfo struct {
	Name         string `json:"name"`
	Matcher      string `json:"matcher,omitempty"`
	Decision     string `json:"decision"`
	Halt         bool   `json:"halt,omitempty"`
	Reason       string `json:"reason,omitempty"`
	InputRewrite bool   `json:"input_rewrite,omitempty"`
}

// Decision represents the outcome of a single hook execution.
type Decision int

const (
	// DecisionNone means the hook expressed no opinion.
	DecisionNone Decision = iota
	// DecisionAllow means the hook explicitly allowed the action.
	DecisionAllow
	// DecisionDeny means the hook blocked the action.
	DecisionDeny
)

func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionDeny:
		return "deny"
	default:
		return "none"
	}
}

// HookResult holds the parsed output of a single hook execution.
type HookResult struct {
	Decision        Decision
	Halt            bool   // If true, halt the whole turn.
	Reason          string // Deny or halt reason (same field, different audience).
	Context         string
	UpdatedInput    string // Shallow-merge patch against tool_input (opaque JSON).
	RequiredFailure bool   // A required hook could not execute successfully.
}

// AggregateResult holds the combined outcome of all hooks for an event.
type AggregateResult struct {
	Decision        Decision
	Halt            bool       // Any hook requested halt.
	HookCount       int        // Number of hooks that ran.
	Hooks           []HookInfo // Info about each hook that ran (config order).
	Reason          string     // Concatenated deny/halt reasons (newline-separated).
	Context         string     // Concatenated context from all hooks.
	UpdatedInput    string     // Merged tool_input JSON (empty if no patches).
	RequiredFailure bool       // Any required hook failed to execute.
}

// aggregate merges multiple HookResults into a single AggregateResult.
// Results are processed in config order (the order of the slice). Deny
// wins over allow, allow wins over none. Halt is sticky. Reasons and
// context concatenate in order. updated_input patches shallow-merge in
// order against the original tool input; later patches override earlier
// ones on colliding keys.
func aggregate(results []HookResult, origToolInput string) AggregateResult {
	var (
		decision        Decision
		halt            bool
		requiredFailure bool
		reasons         []string
		contexts        []string
		merged          = origToolInput
		anyPatch        = false
	)
	for _, r := range results {
		requiredFailure = requiredFailure || r.RequiredFailure
		switch r.Decision {
		case DecisionDeny:
			decision = DecisionDeny
			if r.Reason != "" {
				reasons = append(reasons, r.Reason)
			}
		case DecisionAllow:
			if decision != DecisionDeny {
				decision = DecisionAllow
			}
		case DecisionNone:
			// No change.
		}
		if r.Halt {
			halt = true
			if r.Reason != "" && r.Decision != DecisionDeny {
				// A halting hook that didn't also deny still contributes
				// its reason so the user sees it.
				reasons = append(reasons, r.Reason)
			}
		}
		if r.Context != "" {
			contexts = append(contexts, r.Context)
		}
		if r.UpdatedInput != "" {
			next, err := shallowMerge(merged, r.UpdatedInput)
			if err != nil {
				slog.Warn(
					"Hook updated_input patch rejected; ignoring",
					"error", err,
					"patch", r.UpdatedInput,
				)
				continue
			}
			merged = next
			anyPatch = true
		}
	}

	agg := AggregateResult{
		Decision:        decision,
		Halt:            halt,
		HookCount:       len(results),
		RequiredFailure: requiredFailure,
	}
	if anyPatch {
		agg.UpdatedInput = merged
	}
	if len(reasons) > 0 {
		agg.Reason = strings.Join(reasons, "\n")
	}
	if len(contexts) > 0 {
		agg.Context = strings.Join(contexts, "\n")
	}
	return agg
}

// shallowMerge applies a top-level-keys patch to base (both JSON
// objects). Keys in patch overwrite keys in base; keys absent from the
// patch are preserved. Returns an error if either value is not a valid
// JSON object.
func shallowMerge(base, patch string) (string, error) {
	if base == "" {
		base = "{}"
	}
	// Ensure base is an object so sjson has somewhere to write.
	var baseAny any
	if err := json.Unmarshal([]byte(base), &baseAny); err != nil {
		return "", err
	}
	if _, ok := baseAny.(map[string]any); !ok {
		return "", errNotObject("tool_input")
	}
	var patchMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(patch), &patchMap); err != nil {
		return "", errNotObject("updated_input")
	}
	out := base
	for k, v := range patchMap {
		next, err := sjson.SetRawBytes([]byte(out), k, v)
		if err != nil {
			return "", err
		}
		out = string(next)
	}
	return out, nil
}

type errNotObject string

func (e errNotObject) Error() string { return string(e) + " is not a JSON object" }
