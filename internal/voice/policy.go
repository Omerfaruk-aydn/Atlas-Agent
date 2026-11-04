package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

// ToolDecision is what the voice policy allows for one tool call.
type ToolDecision int

// Decisions.
const (
	// ToolAllow runs the call through the normal permission checks.
	ToolAllow ToolDecision = iota
	// ToolConfirm asks the caller first; a spoken yes pre-approves only
	// that one call.
	ToolConfirm
	// ToolBlock never runs the call from a phone turn.
	ToolBlock
)

// defaultBlocked are tools no phone call may run: local code execution,
// file edits, desktop control and recursive calling. Text read from mail or
// web pages reaches the model during a call, so these stay out of reach no
// matter what a confirmation says.
var defaultBlocked = []string{
	"bash", "job_kill", "computer", "isolated_desktop", "android", "remote_*",
	"screen_mark", "app_loop", "local_app", "write", "edit", "multiedit",
	"lsp_rename", "lsp_rename_file", "lsp_replace_symbol", "lsp_edit_plan",
	"atlas_config", "skill_manage", "worktree", "agent_jobs", "call_owner",
}

// mailSendPatterns match tools that send mail. They are blocked unless
// AllowEmailSend is set; drafts stay available behind confirmation.
var mailSendPatterns = []string{"*send*mail*", "*mail*send*", "*send_email*", "*send_message*", "*reply*mail*", "*mail*reply*"}

// ToolPolicy classifies tool calls made during a phone turn.
type ToolPolicy struct {
	allow    []string
	block    []string
	mail     []string
	readOnly func(string) bool
}

// NewToolPolicy builds a policy. readOnly reports the tools Atlas itself
// classifies as read-only; they run without confirmation.
func NewToolPolicy(cfg ToolPolicyConfig, readOnly func(string) bool) ToolPolicy {
	p := ToolPolicy{
		allow:    lowerAll(cfg.Allow),
		block:    append(lowerAll(cfg.Block), defaultBlocked...),
		readOnly: readOnly,
	}
	if !cfg.AllowEmailSend {
		p.mail = mailSendPatterns
	}
	return p
}

func lowerAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func matchAny(patterns []string, name string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool {
		ok, err := path.Match(p, name)
		return err == nil && ok
	})
}

// Decide classifies a tool by name. Block wins over allow, so a user
// allow-list entry cannot reopen a default block.
func (p ToolPolicy) Decide(name string) ToolDecision {
	n := strings.ToLower(name)
	if matchAny(p.block, n) || matchAny(p.mail, n) {
		return ToolBlock
	}
	if matchAny(p.allow, n) || (p.readOnly != nil && p.readOnly(name)) {
		return ToolAllow
	}
	return ToolConfirm
}

// ConfirmRequest is a pending spoken confirmation.
type ConfirmRequest struct {
	ToolName string
	Summary  string
}

// Confirmer asks the caller to confirm one action and waits for the
// answer. It returns false when the caller declines, says something else,
// stays silent or hangs up.
type Confirmer func(ctx context.Context, req ConfirmRequest) (bool, error)

// Turn carries the voice policy for one agent turn through the context, so
// the tool wrapper can apply it without knowing about calls.
type Turn struct {
	CallID   string
	TurnID   string
	Policy   ToolPolicy
	Confirm  Confirmer
	MaxSteps int

	steps atomic.Int32
}

type turnKey struct{}

// WithTurn attaches a voice turn to ctx.
func WithTurn(ctx context.Context, t *Turn) context.Context {
	return context.WithValue(ctx, turnKey{}, t)
}

// TurnFrom returns the voice turn ctx belongs to, if any.
func TurnFrom(ctx context.Context) (*Turn, bool) {
	t, ok := ctx.Value(turnKey{}).(*Turn)
	return t, ok && t != nil
}

// Steps is the number of tool calls authorized so far.
func (t *Turn) Steps() int { return int(t.steps.Load()) }

// Authorization is the outcome of [Turn.Authorize].
type Authorization struct {
	// Run is false when the call must not run; Reason says why.
	Run bool
	// Confirmed is true when the caller explicitly approved this call.
	Confirmed bool
	Reason    string
}

// Authorize applies the policy to one tool call, asking the caller when
// the policy says so.
func (t *Turn) Authorize(ctx context.Context, toolName, input string) (Authorization, error) {
	if n := t.steps.Add(1); t.MaxSteps > 0 && int(n) > t.MaxSteps {
		return Authorization{Reason: fmt.Sprintf("The tool-call budget for one spoken request (%d) is used up. Answer with what you have.", t.MaxSteps)}, nil
	}
	switch t.Policy.Decide(toolName) {
	case ToolBlock:
		return Authorization{Reason: fmt.Sprintf("%s cannot be used during a phone call. Tell the caller to do this on their computer.", toolName)}, nil
	case ToolAllow:
		return Authorization{Run: true}, nil
	}
	if t.Confirm == nil {
		return Authorization{Reason: "This action needs confirmation and none is possible on this call."}, nil
	}
	ok, err := t.Confirm(ctx, ConfirmRequest{ToolName: toolName, Summary: SummarizeToolCall(toolName, input)})
	if err != nil {
		return Authorization{Reason: "The confirmation could not be completed; the action was not run."}, err
	}
	if !ok {
		return Authorization{Reason: "The caller did not confirm this action, so it was not run. Do not retry it unless they ask again."}, nil
	}
	return Authorization{Run: true, Confirmed: true}, nil
}

// summaryKeys are read out first, in this order, so a calendar event's
// date, time, duration and attendees are always part of the confirmation.
var summaryKeys = []string{
	"summary", "title", "subject", "name", "date", "start", "start_time", "startTime",
	"end", "end_time", "endTime", "duration", "duration_minutes", "timezone", "time_zone", "timeZone",
	"attendees", "participants", "to", "recipient", "recipients", "location",
}

const maxSummaryRunes = 320

// SummarizeToolCall renders a tool call as a short spoken description.
func SummarizeToolCall(toolName, input string) string {
	label := strings.ReplaceAll(strings.TrimPrefix(toolName, "mcp_"), "_", " ")
	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err != nil || len(params) == 0 {
		return label
	}
	var parts []string
	seen := map[string]bool{}
	for _, k := range summaryKeys {
		if v, ok := params[k]; ok {
			if s := spokenValue(v); s != "" {
				parts = append(parts, k+" "+s)
			}
			seen[k] = true
		}
	}
	if len(parts) == 0 {
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if seen[k] || len(parts) >= 4 {
				continue
			}
			if s := spokenValue(params[k]); s != "" {
				parts = append(parts, k+" "+s)
			}
		}
	}
	out := label
	if len(parts) > 0 {
		out += ", " + strings.Join(parts, ", ")
	}
	if utf8.RuneCountInString(out) > maxSummaryRunes {
		out = string([]rune(out)[:maxSummaryRunes]) + "…"
	}
	return out
}

func spokenValue(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64, bool:
		return fmt.Sprint(x)
	case []any:
		var items []string
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				if email, ok := m["email"].(string); ok {
					items = append(items, email)
					continue
				}
			}
			if s := spokenValue(e); s != "" {
				items = append(items, s)
			}
		}
		return strings.Join(items, ", ")
	case map[string]any:
		for _, k := range []string{"dateTime", "date_time", "date", "email"} {
			if s, ok := x[k].(string); ok {
				return s
			}
		}
	}
	return ""
}

// Answer is how a caller replied to a confirmation.
type Answer int

// Answers.
const (
	AnswerUnclear Answer = iota
	AnswerYes
	AnswerNo
)

var (
	yesWords = []string{"evet", "onaylıyorum", "onayla", "onaylarım", "onaylıyom", "tamam", "olur", "yes", "yeah", "yep", "confirm", "confirmed", "sure", "okay", "ok"}
	noWords  = []string{"hayır", "hayir", "yok", "değil", "degil", "iptal", "vazgeç", "vazgec", "dur", "no", "nope", "not", "don't", "dont", "cancel", "stop", "ama", "but"}
)

// ParseAnswer classifies a short reply. Anything with a negation or a
// qualification, or longer than a few words, is not a yes.
func ParseAnswer(text string) Answer {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	})
	if len(words) == 0 {
		return AnswerUnclear
	}
	yes := false
	for _, w := range words {
		if slices.Contains(noWords, w) {
			return AnswerNo
		}
		if slices.Contains(yesWords, w) {
			yes = true
		}
	}
	if yes && len(words) <= 4 {
		return AnswerYes
	}
	return AnswerUnclear
}
