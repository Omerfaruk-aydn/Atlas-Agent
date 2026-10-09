package browser

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/chromedp/chromedp"
)

//go:embed target_lib.js
var targetLibScript string

//go:embed fields.js
var fieldsScript string

// Field result statuses separate "typed" from "applied": only a readback
// after the commit settles can establish what the application accepted.
const (
	FieldApplied    = "applied"
	FieldNormalized = "normalized"
	FieldAdjusted   = "adjusted"
	FieldNotApplied = "not_applied"
	FieldUnknown    = "unknown"
	// FieldTyped means commit "none" was requested: the text is in the
	// field, and nothing yet shows whether the application will keep it.
	FieldTyped = "typed_uncommitted"
)

// DefaultFieldSettle is the observation window after a commit when the
// request does not name one. See SetField for what the window means.
const DefaultFieldSettle = 300 * time.Millisecond

// fieldQuiet is how long a value must stay unchanged after its last change
// before the window may end, so a change at the end of the window is not
// taken as the answer before the application had a chance to answer it.
const fieldQuiet = 250 * time.Millisecond

// FieldTarget names the element a field operation resolves. Within scopes
// the target to a container identified by its accessible name or heading.
type FieldTarget struct {
	Selector   string `json:"selector,omitempty"`
	Role       string `json:"role,omitempty"`
	Name       string `json:"name,omitempty"`
	Label      string `json:"label,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Within     string `json:"within,omitempty"`
	WithinRole string `json:"within_role,omitempty"`
	Match      string `json:"match,omitempty"`
}

// FieldTargetFrom converts the advanced request's target fields.
func FieldTargetFrom(p Request) FieldTarget {
	return FieldTarget{Selector: p.Selector, Role: p.Role, Name: p.Name, Label: p.Label, Scope: p.Scope, Within: p.Within, WithinRole: p.WithinRole, Match: p.Match}
}

func (t FieldTarget) empty() bool {
	return t.Selector == "" && t.Role == "" && t.Name == "" && t.Label == ""
}

// FieldIdentity is what a field edited: the tab, frame and document it
// lives in, and fingerprints of the page's selected objects and of the other
// fields in its panel. A property panel reused for another selected object
// keeps its labels but changes the fingerprints. Values are kept only as
// hashes and never reach the model.
type FieldIdentity struct {
	TabID      string
	FrameID    string
	DocumentID string
	Selection  string
	Scope      string
}

// Mismatch names the first part of b that differs from a, or "" when both
// describe the same field of the same object.
func (a FieldIdentity) Mismatch(b FieldIdentity) string {
	switch {
	case a.TabID != b.TabID:
		return "tab"
	case a.FrameID != b.FrameID:
		return "frame"
	case a.DocumentID == "" || a.DocumentID != b.DocumentID:
		return "document"
	case a.Selection != b.Selection:
		return "selection"
	case a.Scope != b.Scope:
		return "panel"
	}
	return ""
}

// FieldResult reports each stage of a field change: what was there, what
// was typed, which commit was used and what the application kept.
type FieldResult struct {
	Role           string   `json:"role,omitempty"`
	Name           string   `json:"name,omitempty"`
	Context        string   `json:"context,omitempty"`
	Kind           string   `json:"kind"`
	Before         string   `json:"before"`
	Requested      string   `json:"requested"`
	Expected       string   `json:"expected_value,omitempty"`
	Typed          string   `json:"typed_value,omitempty"`
	Accepted       string   `json:"accepted_value"`
	AcceptedOption string   `json:"accepted_option_value,omitempty"`
	Status         string   `json:"status"`
	Commit         string   `json:"commit"`
	CommitAttempts []string `json:"commit_attempts,omitempty"`
	NodeReplaced   bool     `json:"node_replaced,omitempty"`
	IdentityLost   bool     `json:"identity_changed,omitempty"`
	// RelabeledAs is the name the edited node carries after the commit
	// when that is another name (Figma renames "Vertical resizing" to
	// "Height" once a size is fixed).
	RelabeledAs  string `json:"relabeled_as,omitempty"`
	Valid        *bool  `json:"valid,omitempty"`
	Message      string `json:"validation_message,omitempty"`
	Dispatch     string `json:"dispatch"`
	Verification string `json:"verification"`
	// Acceptance names the criterion behind the status: stable_window (the
	// value held through the window after the commit) or read (no input).
	Acceptance string `json:"acceptance,omitempty"`
	ObservedMS int64  `json:"observed_ms,omitempty"`
	StableMS   int64  `json:"stable_ms,omitempty"`
	FrameID    string `json:"frame_id,omitempty"`
	DocumentID string `json:"document_id,omitempty"`
	Note       string `json:"note,omitempty"`
	// Identity is kept for undo and is never serialized.
	Identity FieldIdentity `json:"-"`
}

// Succeeded reports whether the requested value is what the page holds.
func (r FieldResult) Succeeded() bool {
	return r.Status == FieldApplied || r.Status == FieldNormalized
}

// FieldSession is implemented by drivers that can commit and read back
// form and property-panel fields.
type FieldSession interface {
	SetField(ctx context.Context, p Request) (FieldResult, error)
	ReadField(ctx context.Context, p Request) (FieldResult, error)
}

// ErrFieldNotApplied marks a readback that does not hold the requested
// value. The result accompanying it says what the page holds instead.
var ErrFieldNotApplied = errors.New("value_not_applied")

var fieldTokens atomic.Uint64

func fieldToken() string {
	return fmt.Sprintf("f%d-%d", time.Now().UnixNano()%1_000_000_000, fieldTokens.Add(1))
}

// FieldCommitPlan chooses commit methods for a field kind. Native controls
// commit through their own events; multi-line editors and fields inside a
// form are blurred so Enter cannot add a newline or submit the form; panel
// inputs outside forms try Enter, then blur. A learned method observed to
// work on this origin goes first but is still verified by readback.
func FieldCommitPlan(kind string, inForm bool, requested string, learned string) []string {
	switch requested {
	case "enter", "tab", "blur", "none":
		return []string{requested}
	}
	switch {
	case kind == "select" || kind == "checkbox" || kind == "radio" || strings.HasPrefix(kind, "native_"):
		return []string{"event"}
	case kind == "textarea" || kind == "contenteditable":
		return []string{"blur"}
	}
	plan := []string{"enter", "blur"}
	if inForm {
		plan = []string{"blur", "tab"}
	}
	if learned != "" && learned != plan[0] && (learned == "enter" || learned == "tab" || learned == "blur") && (!inForm || learned != "enter") {
		rest := []string{learned}
		for _, method := range plan {
			if method != learned {
				rest = append(rest, method)
			}
		}
		plan = rest
	}
	return plan
}

type fieldIdentityData struct {
	Selection []string `json:"selection"`
	Scope     []string `json:"scope"`
}

type fieldScriptResult struct {
	OK              bool               `json:"ok"`
	Reason          string             `json:"reason"`
	Count           int                `json:"count"`
	Candidates      []json.RawMessage  `json:"candidates"`
	Available       []json.RawMessage  `json:"available"`
	Role            string             `json:"role"`
	Name            string             `json:"name"`
	Context         string             `json:"context"`
	Kind            string             `json:"kind"`
	InForm          bool               `json:"in_form"`
	Before          string             `json:"before"`
	Expected        *string            `json:"expected"`
	Focused         bool               `json:"focused"`
	Busy            bool               `json:"busy"`
	X               float64            `json:"x"`
	Y               float64            `json:"y"`
	After           string             `json:"after"`
	Connected       bool               `json:"connected"`
	Value           string             `json:"value"`
	OptionValue     *string            `json:"option_value"`
	Label           string             `json:"label"`
	Valid           *bool              `json:"valid"`
	Message         string             `json:"message"`
	ResolvedCount   *int               `json:"resolved_count"`
	ResolvedIsTok   *bool              `json:"resolved_is_token"`
	ResolvedValue   *string            `json:"resolved_value"`
	ResolvedOption  *string            `json:"resolved_option_value"`
	Options         []string           `json:"options"`
	IdentityDetails *fieldIdentityData `json:"identity"`
	NamedBefore     *int               `json:"named_before"`
	Expanded        bool               `json:"expanded"`
	Shown           string             `json:"shown"`
}

// fieldFrame binds every step of one field operation (prepare, input,
// commit, readback, cleanup) to one frame and document. A named frame runs
// in its own isolated world, so a navigation or removal makes the next step
// fail instead of silently running in another document.
type fieldFrame struct {
	frameID   string
	contextID float64
	loaderID  string
	tabID     string
}

func (s *chromedpSession) bindFieldFrame(ctx context.Context, p Request) (fieldFrame, error) {
	if p.ExpectedTabID != "" || p.ExpectedOrigin != "" {
		if err := s.guardBrowserDocument(ctx, Request{ExpectedTabID: p.ExpectedTabID, ExpectedOrigin: p.ExpectedOrigin, FrameID: p.FrameID}); err != nil {
			return fieldFrame{}, err
		}
	}
	tree, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false)
	if err != nil {
		return fieldFrame{}, s.explainFailure(err)
	}
	frame := browserFrameInfo(tree, p.FrameID)
	if frame == nil {
		if s.outOfProcessFrame(ctx, p.FrameID) {
			return fieldFrame{}, fmt.Errorf("frame_unreachable: frame %s is a cross-origin frame running in its own process; field actions reach only frames of the tab's own process. Nothing was changed. Open the frame's page in its own tab if the task needs it", p.FrameID)
		}
		return fieldFrame{}, fmt.Errorf("stale_frame: frame %s is not in the current tab (closed, replaced or from another tab); nothing was changed. List frames and resolve the field again", p.FrameID)
	}
	bound := fieldFrame{frameID: p.FrameID, tabID: s.ActiveTab()}
	bound.loaderID, _ = frame["loaderId"].(string)
	if p.DocumentID != "" && bound.loaderID != p.DocumentID {
		return fieldFrame{}, errors.New("stale_document: the frame now holds another document (it navigated or reloaded); nothing was changed. Observe it and resolve the field again")
	}
	if p.FrameID != "" {
		world, err := s.command(ctx, "Page.createIsolatedWorld", map[string]any{"frameId": p.FrameID, "worldName": "atlas-fields"}, false)
		if err != nil {
			return fieldFrame{}, fmt.Errorf("stale_frame: frame %s cannot be entered (%v); nothing was changed", p.FrameID, err)
		}
		bound.contextID, _ = world["executionContextId"].(float64)
		if bound.contextID == 0 {
			return fieldFrame{}, fmt.Errorf("stale_frame: frame %s has no execution context; nothing was changed", p.FrameID)
		}
	}
	return bound, nil
}

// outOfProcessFrame reports whether id names a frame the browser runs as a
// separate target, which the tab's own frame tree does not contain.
func (s *chromedpSession) outOfProcessFrame(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	targets, err := s.command(ctx, "Target.getTargets", map[string]any{}, true)
	if err != nil {
		return false
	}
	infos, _ := targets["targetInfos"].([]any)
	for _, item := range infos {
		info, _ := item.(map[string]any)
		if info["targetId"] == id && info["type"] == "iframe" {
			return true
		}
	}
	return false
}

// verifyFieldDocument checks that the bound frame still holds the document
// the operation started in.
func (s *chromedpSession) verifyFieldDocument(ctx context.Context, f fieldFrame) error {
	if s.ActiveTab() != f.tabID {
		return errors.New("wrong_tab: the selected tab changed during the field operation")
	}
	tree, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false)
	if err != nil {
		return s.explainFailure(err)
	}
	frame := browserFrameInfo(tree, f.frameID)
	if frame == nil {
		return errors.New("stale_frame: the frame closed during the field operation")
	}
	if loader, _ := frame["loaderId"].(string); loader != f.loaderID {
		return errors.New("stale_document: the document was replaced during the field operation")
	}
	return nil
}

func (s *chromedpSession) fieldScript(ctx context.Context, f fieldFrame, params map[string]any) (fieldScriptResult, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return fieldScriptResult{}, err
	}
	script := strings.Replace(strings.Replace(fieldsScript, "__TARGET_LIB__", targetLibScript, 1), "__PARAMS__", string(encoded), 1)
	args := map[string]any{"expression": script, "returnByValue": true, "awaitPromise": true}
	if f.contextID != 0 {
		args["contextId"] = f.contextID
	}
	op, _ := params["op"].(string)
	var out fieldScriptResult
	for {
		v, err := s.command(ctx, "Runtime.evaluate", args, false)
		if err != nil {
			if transientBrowserContextError(err) {
				// A named frame's world is gone only when its document is:
				// never continue in whatever document replaced it.
				if f.frameID != "" {
					return out, fmt.Errorf("stale_frame: the frame navigated or closed during the field operation (%s)", op)
				}
				// The top document may be between two documents; only steps
				// that send nothing are repeated, and the document check
				// afterwards refuses a result from a new document.
				if op == "prepare" || op == "resolve" {
					if err := browserPoll(ctx); err != nil {
						return out, err
					}
					continue
				}
				return out, errors.New("stale_document: the page navigated during the field operation")
			}
			return out, s.explainFailure(err)
		}
		if v["exceptionDetails"] != nil {
			return out, errors.New("target_missing: field evaluation failed; observe the page and resolve the field again")
		}
		result, _ := v["result"].(map[string]any)
		data, _ := json.Marshal(result["value"])
		err = json.Unmarshal(data, &out)
		return out, err
	}
}

func (s *chromedpSession) fieldContext(parent context.Context, p Request) (context.Context, context.CancelFunc, error) {
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	if p.TimeoutMS < 0 || p.TimeoutMS > 30000 {
		return nil, nil, errors.New("timeout_ms must be 0-30000")
	}
	timeout := 15 * time.Second
	if p.TimeoutMS > 0 {
		timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(s.currentContext(), timeout)
	stop := context.AfterFunc(parent, cancel)
	return ctx, func() { stop(); cancel() }, nil
}

// fieldError keeps the caller's cancellation visible: when the parent was
// canceled the result carries context.Canceled rather than a timeout text.
func fieldError(parent context.Context, err error) error {
	if err != nil && parent.Err() != nil && !errors.Is(err, parent.Err()) {
		return fmt.Errorf("%w: %v", parent.Err(), err)
	}
	return err
}

func fieldTargetError(r fieldScriptResult) error {
	switch {
	case strings.HasPrefix(r.Reason, "password"):
		return errors.New("password_field: use vault_fill for saved credentials; passwords never go through field actions")
	case r.Reason == "ambiguous_target" || r.Reason == "within_not_unique":
		data, _ := json.Marshal(r.Candidates)
		return fmt.Errorf("ambiguous_target: %d candidates %s; narrow with within, scope, role or exact name", r.Count, data)
	case r.Reason == "within_not_found":
		data, _ := json.Marshal(r.Available)
		return fmt.Errorf("within_not_found: no visible container is named like within; nothing was sent. Visible named containers: %s. Use one of them (plain label or the role \"label\" form) or drop within", data)
	case r.Reason == "scope_not_unique":
		return fmt.Errorf("target_resolution_failed: scope matches %d containers; refine it", r.Count)
	case r.Reason != "":
		if strings.Contains(r.Reason, ":") {
			return errors.New(r.Reason)
		}
		if r.Reason == "target_missing" {
			if len(r.Candidates) > 0 {
				data, _ := json.Marshal(r.Candidates)
				return fmt.Errorf("target_missing: no field has that label or name; nothing was sent. Similar visible fields: %s. Target one of them by name (or its ref)", data)
			}
			return errors.New("target_missing: no visible field has that label or name; nothing was sent. Observe or find the field and use its exact name, label or ref")
		}
		return fmt.Errorf("%s: resolve the field again from a fresh observation", r.Reason)
	}
	return errors.New("target_missing: field not found")
}

func hashFieldParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:12])
}

func (f fieldFrame) identity(details *fieldIdentityData) FieldIdentity {
	id := FieldIdentity{TabID: f.tabID, FrameID: f.frameID, DocumentID: f.loaderID}
	if details != nil {
		id.Selection, id.Scope = hashFieldParts(details.Selection), hashFieldParts(details.Scope)
	}
	return id
}

// ReadField resolves a field and reports its current value without input.
func (s *chromedpSession) ReadField(parent context.Context, p Request) (FieldResult, error) {
	target := FieldTargetFrom(p)
	if target.empty() {
		return FieldResult{}, errors.New("read_field requires a selector, ref or semantic target")
	}
	ctx, cancel, err := s.fieldContext(parent, p)
	if err != nil {
		return FieldResult{}, err
	}
	defer cancel()
	restore, err := s.suspendActivity()
	if err != nil {
		return FieldResult{}, err
	}
	defer restore()
	frame, err := s.bindFieldFrame(ctx, p)
	if err != nil {
		return FieldResult{}, fieldError(parent, err)
	}
	r, err := s.fieldScript(ctx, frame, map[string]any{"op": "resolve", "target": target})
	if err != nil {
		return FieldResult{}, fieldError(parent, err)
	}
	if !r.OK {
		return FieldResult{}, fieldTargetError(r)
	}
	if err := s.verifyFieldDocument(ctx, frame); err != nil {
		return FieldResult{}, fieldError(parent, err)
	}
	res := FieldResult{Role: r.Role, Name: r.Name, Context: r.Context, Kind: r.Kind, Before: r.Before, Accepted: r.Before, Status: "read", Commit: "none", Valid: r.Valid, Message: r.Message, Dispatch: "none", Verification: "structural", Acceptance: "read", FrameID: frame.frameID, DocumentID: frame.loaderID, Identity: frame.identity(r.IdentityDetails)}
	if r.OptionValue != nil {
		res.AcceptedOption = *r.OptionValue
	}
	return res, nil
}

// SetField writes a value into one resolved field, commits it with a
// method suited to the field and reads back what the application kept.
//
// After each commit the field is observed for the settle window (settle_ms,
// default 300 ms, never shortened): the result is the value that held from
// its last change for at least 250 ms once the window ended, while neither
// the field nor its container is aria-busy. A value still changing well past
// the window is unknown, not applied. A finite window shows only that the
// application kept the value that long; it cannot rule out later changes.
// Typing alone never counts as applied.
func (s *chromedpSession) SetField(parent context.Context, p Request) (FieldResult, error) {
	res, err := s.setField(parent, p)
	return res, fieldError(parent, err)
}

func (s *chromedpSession) setField(parent context.Context, p Request) (FieldResult, error) {
	target := FieldTargetFrom(p)
	if target.empty() {
		return FieldResult{}, errors.New("set_field requires a selector, ref or semantic target")
	}
	mode := p.Mode
	if mode == "" {
		mode = "replace"
	}
	if mode != "replace" && mode != "append" && mode != "clear" && mode != "replace_selection" {
		return FieldResult{}, errors.New("mode must be replace, append, clear or replace_selection")
	}
	value := p.Value
	if value == "" && mode != "clear" {
		value = p.Text
	}
	if mode == "clear" {
		value = ""
	}
	if value == "" && (mode == "replace" || mode == "append") {
		// Nothing would be typed, and the commit would report the old value
		// as if it were the requested one.
		return FieldResult{Dispatch: "none", Status: FieldUnknown}, errors.New("missing_value: set_field has no value to " + mode + "; give value, or use mode clear to empty the field. Nothing was sent")
	}
	if len(value) > 64*1024 {
		return FieldResult{}, errors.New("field value exceeds 64 KiB")
	}
	if p.Commit != "" && p.Commit != "auto" && p.Commit != "enter" && p.Commit != "tab" && p.Commit != "blur" && p.Commit != "none" {
		return FieldResult{}, errors.New("commit must be auto, enter, tab, blur or none")
	}
	if p.SettleMS < 0 || p.SettleMS > 5000 {
		return FieldResult{}, errors.New("settle_ms must be 0-5000")
	}
	settle := DefaultFieldSettle
	if p.SettleMS > 0 {
		settle = time.Duration(p.SettleMS) * time.Millisecond
	}
	ctx, cancel, err := s.fieldContext(parent, p)
	if err != nil {
		return FieldResult{}, err
	}
	defer cancel()
	if err := s.recoverInput(ctx); err != nil {
		return FieldResult{Dispatch: "none", Status: FieldUnknown}, err
	}
	frame, err := s.bindFieldFrame(ctx, p)
	if err != nil {
		return FieldResult{Dispatch: "none", Status: FieldUnknown}, err
	}
	token := fieldToken()
	defer func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer done()
		_, _ = s.fieldScript(cleanup, frame, map[string]any{"op": "cleanup", "token": token})
	}()
	prep, err := s.fieldScript(ctx, frame, map[string]any{"op": "prepare", "target": target, "token": token, "mode": mode, "value": value})
	if err != nil {
		return FieldResult{Dispatch: "none", Status: FieldUnknown}, err
	}
	if !prep.OK {
		return FieldResult{Dispatch: "none", Status: FieldUnknown}, fieldTargetError(prep)
	}
	result := FieldResult{Role: prep.Role, Name: prep.Name, Context: prep.Context, Kind: prep.Kind, Before: prep.Before, Requested: value, Dispatch: "none", Verification: "unknown", FrameID: frame.frameID, DocumentID: frame.loaderID, Status: FieldUnknown}
	expected := value
	switch mode {
	case "append", "replace_selection":
		if prep.Expected == nil {
			return result, errors.New("unsupported_field_mode: the field's current selection could not be read, so the resulting value is unknown; nothing was typed. Use mode replace with the whole value")
		}
		expected = *prep.Expected
		result.Expected = expected
	}
	if strings.HasPrefix(prep.Kind, "custom_") || prep.Kind == "other" || prep.Kind == "file" {
		return result, fmt.Errorf("unsupported_field_kind: %s fields need select_option, a click or upload; nothing was typed", prep.Kind)
	}
	if err := s.verifyFieldDocument(ctx, frame); err != nil {
		return result, err
	}
	send := s.inputSender()
	plan := FieldCommitPlan(prep.Kind, prep.InForm, p.Commit, p.LearnedCommit)
	var readback fieldScriptResult
	var settled fieldSettle
	if plan[0] == "event" {
		applied, err := s.fieldScript(ctx, frame, map[string]any{"op": "apply_native", "token": token, "value": value})
		if err != nil {
			return result, err
		}
		if !applied.OK {
			if len(applied.Options) > 0 {
				return result, fmt.Errorf("%s: available options %q", applied.Reason, applied.Options)
			}
			return result, errors.New(applied.Reason)
		}
		result.Dispatch, result.Commit, result.CommitAttempts = "sent", "event", []string{"event"}
		if readback, settled, err = s.settleField(ctx, frame, token, target, settle, "event"); err != nil {
			return result, err
		}
		s.applyFieldReadback(&result, readback, expected, settled)
	} else {
		if !prep.Focused {
			return result, errors.New("field_not_ready: the field did not take keyboard focus; nothing was typed. Observe what covers or replaces it")
		}
		events := []inputEvent{}
		if mode == "clear" {
			key, _ := resolveInputKey("backspace", false)
			events = append(events, keyEvent("rawKeyDown", key, 0, ""), keyEvent("keyUp", key, 0, ""))
		} else if value != "" {
			events = append(events, inputEvent{method: cdpInsertText, params: map[string]any{"text": value}})
		}
		if len(events) > 0 {
			if _, err := s.dispatchSessionInput(ctx, send, events); err != nil {
				result.Dispatch = "unknown"
				return result, s.explainFailure(err)
			}
		}
		result.Dispatch = "sent"
		echo, err := s.fieldScript(ctx, frame, map[string]any{"op": "read", "token": token})
		if err != nil {
			return result, err
		}
		result.Typed = echo.Value
		for attempt, method := range plan {
			if attempt > 0 {
				// A second method only runs when the first left the old
				// value in place; text the app discarded is typed again
				// rather than assumed.
				current, err := s.fieldScript(ctx, frame, map[string]any{"op": "read", "token": token})
				if err != nil || !current.Connected {
					break
				}
				if current.Value != expected {
					again, err := s.fieldScript(ctx, frame, map[string]any{"op": "prepare", "target": target, "token": token, "mode": mode, "value": value})
					if err != nil || !again.OK || !again.Focused {
						break
					}
					if value != "" {
						if _, err := s.dispatchSessionInput(ctx, send, []inputEvent{{method: cdpInsertText, params: map[string]any{"text": value}}}); err != nil {
							result.Dispatch = "unknown"
							return result, s.explainFailure(err)
						}
					}
				}
			}
			result.CommitAttempts = append(result.CommitAttempts, method)
			result.Commit = method
			if err := s.commitField(ctx, frame, send, token, method); err != nil {
				return result, err
			}
			if readback, settled, err = s.settleField(ctx, frame, token, target, settle, method); err != nil {
				return result, err
			}
			if method == "enter" && readback.Connected && readback.Focused && readback.Value == result.Typed {
				// The field still has focus and shows exactly what was typed:
				// nothing proves the key committed it. Releasing focus lets
				// the application finalize the edit before the readback.
				result.CommitAttempts = append(result.CommitAttempts, "blur")
				result.Commit = "enter+blur"
				if err := s.commitField(ctx, frame, send, token, "blur"); err != nil {
					return result, err
				}
				if readback, settled, err = s.settleField(ctx, frame, token, target, settle, "blur"); err != nil {
					return result, err
				}
			}
			s.applyFieldReadback(&result, readback, expected, settled)
			if method == "none" {
				result.Status = FieldTyped
			}
			if result.Status != FieldNotApplied || (result.Valid != nil && !*result.Valid) {
				break
			}
		}
	}
	if err := s.verifyFieldDocument(ctx, frame); err != nil {
		result.Status, result.Verification = FieldUnknown, "unknown"
		return result, fmt.Errorf("outcome_unknown: %w; observe before retrying so the value is not applied twice", err)
	}
	if result.Status != FieldTyped {
		final, err := s.fieldScript(ctx, frame, map[string]any{"op": "read", "token": token, "target": target, "identity": true})
		if err == nil {
			result.Identity = frame.identity(final.IdentityDetails)
		}
	}
	switch result.Status {
	case FieldApplied, FieldNormalized:
		result.Verification = "verified"
	case FieldTyped:
		result.Verification = "not_committed"
		result.Note = "Text is in the field but was not committed. Commit it (or call set_field with a commit) before relying on the value."
	case FieldAdjusted:
		result.Verification = "differs"
		result.Note = "The application changed the value (format, limit or correction). Decide whether the accepted value meets the request."
	case FieldNotApplied:
		result.Verification = "failed"
		return result, fmt.Errorf("%w: the field holds %q after %s and a %dms window; it was typed but not kept. Observe the field and choose a different commit or route; do not repeat the same input", ErrFieldNotApplied, result.Accepted, strings.Join(result.CommitAttempts, ", then "), result.ObservedMS)
	default:
		result.Verification = "unknown"
		if !settled.unstable {
			if recovered, recoverySettle, ok := s.resolveRenamedField(ctx, frame, prep, target, expected, settle); ok {
				result.Role, result.Name, result.Accepted = recovered.Role, recovered.Name, recovered.Before
				result.Valid, result.Message = recovered.Valid, recovered.Message
				result.ObservedMS += recoverySettle.observed.Milliseconds()
				result.StableMS = recoverySettle.stable.Milliseconds()
				result.Identity = frame.identity(recovered.IdentityDetails)
				result.NodeReplaced, result.IdentityLost = true, false
				result.Status = CompareTypedFieldValue(result.Kind, result.Name, expected, recovered.Before, result.Before)
				result.Acceptance = "renamed_readback"
				result.Note = "The application replaced or renamed the field after the commit (its name carries the value). The same property of the same selection was resolved again and read back; the write was not repeated."
				if recovered.Name == prep.Name {
					result.Acceptance = "remount_readback"
					result.Note = "The application rebuilt the field after the commit. The field with the same name for the same selection was resolved again and read back; the write was not repeated."
				}
				if recovered.Valid != nil && !*recovered.Valid {
					result.Status, result.Verification = FieldNotApplied, "failed"
					return result, fmt.Errorf("%w: the rebuilt field rejects the value: %s", ErrFieldNotApplied, recovered.Message)
				}
				if result.Succeeded() {
					result.Verification = "verified"
					return result, nil
				}
				if result.Status == FieldAdjusted {
					result.Verification = "differs"
					return result, nil
				}
				if result.Status == FieldNotApplied {
					result.Verification = "failed"
					return result, fmt.Errorf("%w: the rebuilt field holds %q instead of %q", ErrFieldNotApplied, result.Accepted, expected)
				}
				result.Status, result.Verification = FieldUnknown, "unknown"
			}
		}
		if !settled.unstable && s.inlineRenameLanded(ctx, frame, token, prep, expected) {
			result.Accepted, result.Status, result.Verification = expected, FieldApplied, "verified"
			result.NodeReplaced, result.IdentityLost = true, false
			result.Acceptance = "inline_rename"
			result.Note = "The rename editor closed on commit; its original parent and child slot now contain a named object with the requested value. The write was not repeated."
			return result, nil
		}
		if settled.unstable {
			return result, fmt.Errorf("outcome_unknown: value_unstable: the field was still changing %dms after the commit (last seen %q); nothing is claimed. Observe it before retrying so the value is not applied twice", result.ObservedMS, result.Accepted)
		}
		if result.RelabeledAs != "" {
			// Not claimed: a new name may be the same property under
			// another label or another property. Name it so one read
			// settles it.
			return result, fmt.Errorf("outcome_unknown: the edited field is now named %q and shows %q; it may be the same property under a new name or another one. Read %q before retrying so the value is not applied twice", result.RelabeledAs, result.Accepted, result.RelabeledAs)
		}
		return result, errors.New("outcome_unknown: the field was replaced, reused for another property or disappeared after input; observe before retrying so the value is not applied twice")
	}
	return result, nil
}

func (s *chromedpSession) applyFieldReadback(result *FieldResult, r fieldScriptResult, expected string, settled fieldSettle) {
	result.ObservedMS, result.StableMS = settled.observed.Milliseconds(), settled.stable.Milliseconds()
	result.Acceptance = "stable_window"
	// Validity is reported only when the page rejects the value.
	result.Valid, result.Message = nil, r.Message
	if r.Valid != nil && !*r.Valid {
		result.Valid = r.Valid
	}
	accepted, option, known := r.Value, r.OptionValue, r.Connected
	result.NodeReplaced = false
	if r.ResolvedCount != nil && *r.ResolvedCount == 1 && r.ResolvedValue != nil {
		accepted, option, known = *r.ResolvedValue, r.ResolvedOption, true
		result.NodeReplaced = r.ResolvedIsTok != nil && !*r.ResolvedIsTok
	} else if !r.Connected || (r.ResolvedCount != nil && *r.ResolvedCount != 1) {
		// The edited node is gone, or the target no longer names exactly one
		// field: nothing identifies what holds the value now.
		known = false
	}
	result.IdentityLost, result.RelabeledAs = false, ""
	if r.Connected && r.Label != "" && result.Name != "" && !strings.EqualFold(r.Label, result.Name) && !strings.Contains(strings.ToLower(r.Label), strings.ToLower(result.Name)) {
		// The node now describes another property: the application reused it.
		result.IdentityLost, result.RelabeledAs = true, r.Label
		known = r.ResolvedCount != nil && *r.ResolvedCount == 1 && r.ResolvedIsTok != nil && !*r.ResolvedIsTok
	}
	// A replacement has not been observed for its own full window yet.
	// Resolve it with the original selection before claiming success.
	if result.NodeReplaced {
		known = false
	}
	result.Accepted = accepted
	result.AcceptedOption = ""
	if option != nil {
		result.AcceptedOption = *option
	}
	if !known || settled.unstable {
		result.Status = FieldUnknown
		return
	}
	if result.Valid != nil && !*result.Valid {
		result.Status = FieldNotApplied
		return
	}
	if result.Kind == "select" {
		result.Status = CompareSelectOption(result.Requested, accepted, result.AcceptedOption, result.Before)
		return
	}
	result.Status = CompareTypedFieldValue(result.Kind, result.Name, expected, accepted, result.Before)
}

func (s *chromedpSession) inputSender() inputSender {
	m, pointerID := s.pointerOwner()
	return func(ctx context.Context, method string, params map[string]any) error {
		if method == cdpMouseEvent && params["type"] == "mousePressed" {
			s.presentMouseAim(m, pointerID, params)
		}
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			c := chromedp.FromContext(ctx)
			if c == nil || c.Target == nil {
				return errors.New("no active browser target")
			}
			return c.Target.Execute(ctx, method, params, nil)
		}))
		if err == nil && method == cdpMouseEvent {
			s.presentMouseEvent(m, pointerID, params)
		}
		return err
	}
}

func (s *chromedpSession) commitField(ctx context.Context, frame fieldFrame, send inputSender, token, method string) error {
	switch method {
	case "enter", "tab":
		key, _ := resolveInputKey(method, false)
		down, text := "rawKeyDown", ""
		if key.text != "" {
			down, text = "keyDown", key.text
		}
		if _, err := s.dispatchSessionInput(ctx, send, []inputEvent{keyEvent(down, key, 0, text), keyEvent("keyUp", key, 0, "")}); err != nil {
			return s.explainFailure(err)
		}
	case "blur":
		if _, err := s.fieldScript(ctx, frame, map[string]any{"op": "blur", "token": token}); err != nil {
			return err
		}
	}
	return nil
}

// fieldSettle describes the observation that produced a readback.
type fieldSettle struct {
	observed time.Duration
	stable   time.Duration
	unstable bool
}

// fieldReadKey is everything a readback can tell apart: the value, which
// node holds it, its label, and whether it is focused or busy. Any change
// restarts the quiet period.
func fieldReadKey(r fieldScriptResult) string {
	part := func(p *string) string {
		if p == nil {
			return "\x01"
		}
		return *p
	}
	count, isToken := -1, false
	if r.ResolvedCount != nil {
		count = *r.ResolvedCount
	}
	if r.ResolvedIsTok != nil {
		isToken = *r.ResolvedIsTok
	}
	return fmt.Sprintf("%t\x00%s\x00%s\x00%d\x00%t\x00%s\x00%s\x00%t\x00%t", r.Connected, r.Value, part(r.OptionValue), count, isToken, part(r.ResolvedValue), r.Label, r.Focused, r.Busy)
}

// settleField observes the field for the whole window after a commit. It
// returns once the window has passed and the field has held still for the
// quiet period, is not busy and (after a blur) no longer has focus. It gives
// up, marking the readback unstable, when that has not happened within the
// window plus max(window, 2s).
func (s *chromedpSession) settleField(ctx context.Context, frame fieldFrame, token string, target FieldTarget, window time.Duration, method string) (fieldScriptResult, fieldSettle, error) {
	started := time.Now()
	lastChange := started
	quiet := min(window, fieldQuiet)
	limit := window + max(window, 2*time.Second)
	var last fieldScriptResult
	lastKey := ""
	for i := 0; ; i++ {
		r, err := s.fieldScript(ctx, frame, map[string]any{"op": "read", "token": token, "target": target})
		if err != nil {
			return last, fieldSettle{observed: time.Since(started)}, err
		}
		key := fieldReadKey(r)
		if i == 0 || key != lastKey {
			lastChange = time.Now()
		}
		last, lastKey = r, key
		elapsed := time.Since(started)
		info := fieldSettle{observed: elapsed, stable: time.Since(lastChange)}
		if elapsed >= window && info.stable >= quiet && !r.Busy && (method != "blur" || !r.Focused) {
			return last, info, nil
		}
		if elapsed >= limit {
			info.unstable = true
			return last, info, nil
		}
		select {
		case <-ctx.Done():
			return last, info, fmt.Errorf("condition_timeout: field readback did not settle within the request's time budget: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// SelectOption chooses an option in a native select or a custom combobox,
// then reads back the displayed value.
func (s *chromedpSession) SelectOption(parent context.Context, p Request) (FieldResult, error) {
	res, err := s.selectOption(parent, p)
	return res, fieldError(parent, err)
}

func (s *chromedpSession) selectOption(parent context.Context, p Request) (FieldResult, error) {
	target := FieldTargetFrom(p)
	option := p.Value
	if option == "" {
		option = p.Text
	}
	if target.empty() || option == "" {
		return FieldResult{}, errors.New("select_option requires a target and value naming the option")
	}
	if p.SettleMS < 0 || p.SettleMS > 5000 {
		return FieldResult{}, errors.New("settle_ms must be 0-5000")
	}
	settle := DefaultFieldSettle
	if p.SettleMS > 0 {
		settle = time.Duration(p.SettleMS) * time.Millisecond
	}
	ctx, cancel, err := s.fieldContext(parent, p)
	if err != nil {
		return FieldResult{}, err
	}
	defer cancel()
	if err := s.recoverInput(ctx); err != nil {
		return FieldResult{Dispatch: "none"}, err
	}
	frame, err := s.bindFieldFrame(ctx, p)
	if err != nil {
		return FieldResult{Dispatch: "none", Status: FieldUnknown}, err
	}
	token := fieldToken()
	defer func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer done()
		_, _ = s.fieldScript(cleanup, frame, map[string]any{"op": "cleanup", "token": token})
	}()
	prep, err := s.fieldScript(ctx, frame, map[string]any{"op": "prepare", "target": target, "token": token, "mode": "click"})
	if err != nil {
		return FieldResult{Dispatch: "none"}, err
	}
	if !prep.OK {
		return FieldResult{Dispatch: "none"}, fieldTargetError(prep)
	}
	result := FieldResult{Role: prep.Role, Name: prep.Name, Context: prep.Context, Kind: prep.Kind, Before: prep.Before, Requested: option, Dispatch: "none", Verification: "unknown", FrameID: frame.frameID, DocumentID: frame.loaderID, Status: FieldUnknown}
	if prep.Kind == "select" {
		applied, err := s.fieldScript(ctx, frame, map[string]any{"op": "apply_native", "token": token, "value": option})
		if err != nil {
			return result, err
		}
		if !applied.OK {
			return result, fmt.Errorf("%s: available options %q", applied.Reason, applied.Options)
		}
		result.Dispatch, result.Commit = "sent", "event"
	} else {
		// Custom combobox: open it with a real click, then click the one
		// visible option with that exact name. The option list is resolved
		// fresh after opening; nothing is chosen by position. Positions from
		// a frame are offset by the frame's place in the page.
		dx, dy, err := s.frameOffset(ctx, frame.frameID)
		if err != nil {
			return result, err
		}
		send := s.inputSender()
		click := func(x, y float64) error {
			x, y = x+dx, y+dy
			_, err := s.dispatchSessionInput(ctx, send, []inputEvent{
				mouseEvent("mouseMoved", x, y, "none", 0, 0, 0),
				mouseEvent("mousePressed", x, y, "left", 1, 1, 0),
				mouseEvent("mouseReleased", x, y, "left", 0, 1, 0),
			})
			return err
		}
		if err := s.verifyFieldDocument(ctx, frame); err != nil {
			return result, err
		}
		// A list the page already shows open (opened by its own trigger,
		// as Figma's dimension menus are) is not clicked again: that click
		// would close it or only focus the text field.
		if !prep.Expanded {
			trigger, err := s.fieldScript(ctx, frame, map[string]any{"op": "option_trigger", "token": token})
			if err != nil {
				return result, err
			}
			if !trigger.OK {
				return result, fieldTargetError(trigger)
			}
			if err := s.verifyFieldDocument(ctx, frame); err != nil {
				return result, err
			}
			if err := click(trigger.X, trigger.Y); err != nil {
				result.Dispatch = "unknown"
				return result, s.explainFailure(err)
			}
			result.Dispatch = "sent"
		}
		var opt fieldScriptResult
		for {
			opt, err = s.fieldScript(ctx, frame, map[string]any{"op": "prepare", "target": FieldTarget{Role: "option", Name: option, Match: firstNonEmptyString(p.Match, "exact")}, "token": token + "o", "mode": "click", "near_token": token})
			if err != nil {
				return result, err
			}
			if opt.OK || opt.Reason == "ambiguous_target" {
				break
			}
			select {
			case <-ctx.Done():
				return result, fmt.Errorf("condition_timeout: option %q did not appear after opening the list; observe the open list", option)
			case <-time.After(100 * time.Millisecond):
			}
		}
		if !opt.OK {
			return result, fieldTargetError(opt)
		}
		if err := click(opt.X, opt.Y); err != nil {
			result.Dispatch = "unknown"
			return result, s.explainFailure(err)
		}
		result.Dispatch, result.Commit = "sent", "click"
	}
	readback, settled, err := s.settleField(ctx, frame, token, target, settle, "event")
	if err != nil {
		return result, err
	}
	s.applyFieldReadback(&result, readback, option, settled)
	if settled.unstable || (result.Valid != nil && !*result.Valid) {
		result.Verification = "failed"
		if settled.unstable {
			result.Status, result.Verification = FieldUnknown, "unknown"
			return result, errors.New("outcome_unknown: option readback did not settle; observe before retrying")
		}
		result.Status = FieldNotApplied
		return result, fmt.Errorf("%w: the control rejects the option: %s", ErrFieldNotApplied, result.Message)
	}
	if prep.Kind != "select" && result.Status == FieldAdjusted && strings.Contains(result.Accepted, option) {
		// A custom combobox may show the chosen option with extra text.
		result.Status = FieldNormalized
	}
	if prep.Kind != "select" && !result.Succeeded() && !result.NodeReplaced && !result.IdentityLost && ShownModeConfirms(prep.Shown, readback.Shown, option) {
		// The field kept its own value and shows the chosen mode beside it.
		result.Status, result.Acceptance = FieldNormalized, "shown_mode"
		result.Note = fmt.Sprintf("The control keeps %q and now shows %q next to it.", result.Accepted, readback.Shown)
	}
	if err := s.verifyFieldDocument(ctx, frame); err != nil {
		result.Status, result.Verification = FieldUnknown, "unknown"
		return result, fmt.Errorf("outcome_unknown: %w", err)
	}
	if !result.Succeeded() {
		result.Verification = "failed"
		return result, fmt.Errorf("%w: the control shows %q, not %q", ErrFieldNotApplied, result.Accepted, option)
	}
	final, err := s.fieldScript(ctx, frame, map[string]any{"op": "read", "token": token, "target": target, "identity": true})
	if err == nil {
		result.Identity = frame.identity(final.IdentityDetails)
	}
	result.Verification = "verified"
	return result, nil
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ShownModeConfirms reports whether the text shown beside a control
// confirms a chosen option the control does not hold itself: the option's
// first word ("Fill" of "Fill container") appears there now and did not
// before. Figma's dimension fields show the number and the mode this way.
func ShownModeConfirms(before, after, option string) bool {
	words := func(text string) []string {
		return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	head := words(option)
	if len(head) == 0 || after == before {
		return false
	}
	has := func(text string) bool { return slices.Contains(words(text), head[0]) }
	return has(after) && !has(before)
}
