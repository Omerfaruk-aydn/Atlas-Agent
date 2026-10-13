package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
)

// browserProcessEpoch distinguishes evidence gathered by this process from
// evidence persisted by an earlier one, whose refs and viewports are gone.
var browserProcessEpoch = fmt.Sprintf("p%d", time.Now().UnixNano())

// browserOpTag makes op ids unique across processes: an op id remembered
// from an earlier process never names an op of this one.
var browserOpTag = strconv.FormatInt(time.Now().UnixNano()%(36*36*36*36), 36)

// browserOpID is the op id for a ledger sequence number.
func browserOpID(seq int) string {
	return fmt.Sprintf("b%d.%s", seq, browserOpTag)
}

// browserStateKey carries the identity under which a browser tool keeps its
// per-session ledger and observation base: the chat session plus the
// session manager serving it, so two managers never share state.
type browserStateKey struct{}

func withBrowserState(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, browserStateKey{}, id)
}

// browserStateID returns the ledger identity for ctx, falling back to the
// chat session when the call did not come through the browser tool.
func browserStateID(ctx context.Context) string {
	if id, ok := ctx.Value(browserStateKey{}).(string); ok && id != "" {
		return id
	}
	return GetSessionFromContext(ctx)
}

// BrowserOp is the shared execution record every browser call reports.
// Dispatch and Verification keep "command accepted", "input sent",
// "application applied" and "effect verified" apart.
type BrowserOp struct {
	ID           string    `json:"op_id"`
	Seq          int       `json:"seq"`
	Action       string    `json:"action"`
	Workflow     string    `json:"workflow_step,omitempty"`
	Tab          string    `json:"tab_id,omitempty"`
	Observation  string    `json:"observation_id,omitempty"`
	Dispatch     string    `json:"dispatch"`
	Verification string    `json:"verification"`
	Cleanup      string    `json:"cleanup,omitempty"`
	DurationMS   int64     `json:"duration_ms"`
	ErrorCode    string    `json:"error_code,omitempty"`
	Recovery     string    `json:"recovery,omitempty"`
	OK           bool      `json:"ok"`
	Target       string    `json:"-"`
	Origin       string    `json:"-"`
	Epoch        string    `json:"-"`
	At           time.Time `json:"-"`
	// Key identifies the exact call (action and parameters); Browser and
	// Document tie the op to one browser connection and top document.
	Key             string   `json:"-"`
	Browser         string   `json:"-"`
	Document        string   `json:"-"`
	TargetKeys      []string `json:"-"`
	ChangedKeys     []string `json:"-"`
	ChangeUncertain bool     `json:"-"`
	// Delivery marks a call that can advance the deliverable itself, as
	// opposed to moving around in it (navigation keys, tool shortcuts,
	// zoom, pointer moves, tab and history changes).
	Delivery bool `json:"-"`
	// Observed is the lowercased text a successful verifying call returned
	// (bounded), so a requirement's expect text can be checked against it.
	Observed string `json:"-"`
	// ObservedRaw preserves case-sensitive source URLs for research evidence.
	ObservedRaw string `json:"-"`
}

// BrowserMetrics are real counters for one chat session; reports quote
// them instead of estimating how many calls were made.
type BrowserMetrics struct {
	Calls       int `json:"calls"`
	Errors      int `json:"errors"`
	Screenshots int `json:"screenshots"`
	OutputChars int `json:"output_chars"`
	Recoveries  int `json:"recovery_attempts"`
	// FirstTry counts calls that succeeded with no failure before them;
	// Recovered counts successes right after a failed call, so a recovery
	// is never reported as first-attempt success.
	FirstTry       int            `json:"succeeded_first_try"`
	Recovered      int            `json:"succeeded_after_failure"`
	BlockedRepeats int            `json:"blocked_repeats"`
	ToolMS         int64          `json:"tool_ms"`
	WaitMS         int64          `json:"wait_ms"`
	ByAction       map[string]int `json:"by_action"`
	ErrorCodes     map[string]int `json:"error_codes,omitempty"`
}

// fieldEdit is one field change Atlas made, with what identifies the edited
// object: its tab, frame and document, and fingerprints of the selection and
// panel at the time. Undo restores it only while all of these still match.
type fieldEdit struct {
	Op             string
	Target         browser.Request
	Kind           string
	Name           string
	Commit         string
	Before         string
	Accepted       string
	AcceptedOption string
	Identity       browser.FieldIdentity
	Origin         string
	At             time.Time
}

type browserLedger struct {
	mu          sync.Mutex
	seq         int
	metrics     BrowserMetrics
	ops         []BrowserOp
	lastFailKey string
	failStreak  int
	lastFailed  bool
	edits       []fieldEdit
	submissions map[string]time.Time
	lastTab     string
	// textEdit is the open canvas text-edit session, if any.
	textEdit *canvasTextEdit
}

var (
	browserLedgersMu sync.Mutex
	browserLedgers   = map[string]*browserLedger{}
)

func ledgerFor(sessionID string) *browserLedger {
	browserLedgersMu.Lock()
	defer browserLedgersMu.Unlock()
	l := browserLedgers[sessionID]
	if l == nil {
		l = &browserLedger{metrics: BrowserMetrics{ByAction: map[string]int{}, ErrorCodes: map[string]int{}}, submissions: map[string]time.Time{}}
		browserLedgers[sessionID] = l
	}
	return l
}

// BrowserSessionMetrics returns a copy of the counters for a session.
func BrowserSessionMetrics(sessionID string) BrowserMetrics {
	l := ledgerFor(sessionID)
	l.mu.Lock()
	defer l.mu.Unlock()
	m := l.metrics
	m.ByAction = map[string]int{}
	for k, v := range l.metrics.ByAction {
		m.ByAction[k] = v
	}
	m.ErrorCodes = map[string]int{}
	for k, v := range l.metrics.ErrorCodes {
		m.ErrorCodes[k] = v
	}
	return m
}

func (l *browserLedger) findOp(id string) (BrowserOp, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.ops) - 1; i >= 0; i-- {
		if l.ops[i].ID == id {
			return l.ops[i], true
		}
	}
	return BrowserOp{}, false
}

// changedAfter reports later writes that invalidate what op verified,
// including uncertain writes and applied fields of a partially failed
// batch. Undo also invalidates earlier field verification.
func (l *browserLedger) changedAfter(op BrowserOp) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, later := range l.ops {
		if later.Seq <= op.Seq {
			continue
		}
		if later.Tab != op.Tab {
			if later.ChangeUncertain || resolvedEvidenceChanged(later.ChangedKeys, op.TargetKeys) {
				return later.ID, true
			}
			continue
		}
		if browserOpInvalidates(later, op.Action, op.Target, op.TargetKeys) {
			return later.ID, true
		}
	}
	return "", false
}

// staleRefGuard accepts only refs resolved by this process in the current
// browser, tab and document. Scoped observations and find results grant
// refs independently of the full-page delta base.
func staleRefGuard(ctx context.Context, sess browser.Session, stateID, action string, params BrowserParams) error {
	if !browserMutating(action) {
		return nil
	}
	refs := []string{params.Ref}
	for _, f := range params.Advanced.Fields {
		refs = append(refs, f.Ref)
	}
	var exec browser.ExecutionIdentity
	checked := false
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		if _, err := resolveTargetSelector(action, BrowserParams{Ref: ref}); err != nil {
			return err
		}
		if !checked {
			exec, checked = referenceIdentity(ctx, sess), true
		}
		if _, strict := sess.(browser.IdentitySession); strict && exec.Document == "" {
			return fmt.Errorf("stale_ref: current document identity is unavailable; observe the page before sending input")
		}
		shown, ok := browserObservations.grantedRef(stateID, sess, exec, ref)
		if !ok {
			return fmt.Errorf("stale_ref: ref %s was not observed in this browser, tab and document by this process; nothing was sent. Observe the page and use a current ref", ref)
		}
		if err := refStillNames(ctx, sess, ref, shown); err != nil {
			return err
		}
	}
	return nil
}

// refStillNames checks, right before input, that the node holding ref is
// still the element the model was shown. Applications reuse nodes for other
// items (virtualized layer rows, panel fields that follow the selection), so
// a ref alone can silently aim at a different object.
func refStillNames(ctx context.Context, sess browser.Session, ref string, shown browser.RefIdentity) error {
	identities, ok := sess.(browser.RefIdentitySession)
	if !ok || shown == (browser.RefIdentity{}) {
		return nil
	}
	current, err := identities.RefIdentities(ctx, []string{ref})
	if err != nil {
		// The input path reports an unreachable page itself.
		return nil
	}
	now, present := current[ref]
	if !present {
		return fmt.Errorf("stale_ref: ref %s (%s %q when observed) is gone from the page; nothing was sent. Observe again and re-aim", ref, shown.Role, shown.Name)
	}
	if now != shown {
		return fmt.Errorf("stale_ref: ref %s was %s %q when observed but is now %s %q; the page reused the element for another item. Nothing was sent. Observe again and target the intended item by its current ref or role/name", ref, shown.Role, shown.Name, now.Role, now.Name)
	}
	return nil
}

// browserMutating reports whether an action can change page or account
// state, which decides dispatch reporting and the repeated-failure guard.
func browserMutating(action string) bool {
	if browserReadOnlyActions[action] {
		return false
	}
	switch action {
	case "observe", "read_field", "health", "task_status", "task_plan", "task_update", "knowledge", "url", "tab_return", "tabs", "research":
		return false
	}
	return true
}

// browserVerifyingActions return page text that can serve as evidence.
var browserVerifyingActions = map[string]bool{
	"layout_audit": true,
	"observe":      true, "snapshot": true, "find": true, "assert": true, "wait_for": true, "read_field": true,
	"set_field": true, "select_option": true, "fill_form": true, "text": true, "html": true,
	"research": true, "collect": true,
}

// maxObservedRunes bounds the evidence text kept per op.
const maxObservedRunes = 24 * 1024

// browserDeliveryAction reports whether a call can change the deliverable.
// Eligibility alone is not proof: browserVerifiedDelivery additionally
// requires a valid committed change. Canvas milestones advance only through
// verified task requirements; selection and navigation are not delivery.
func browserDeliveryAction(action string, params BrowserParams) bool {
	if !browserMutating(action) {
		return false
	}
	switch action {
	case "pointer_move", "navigate", "back", "forward", "tab_new", "tab_select", "tab_close", "tab_return",
		"dialog", "dialog_handle", "close", "handoff", "trace_start", "trace_stop", "export_test", "download_start", "status", "trace":
		return false
	case "key", "key_combo":
		key := strings.ToLower(strings.TrimSpace(firstNonEmpty(params.Key, params.Advanced.Key)))
		if len(params.Advanced.Modifiers) > 0 && !strings.Contains(key, "+") {
			key = strings.ToLower(strings.Join(params.Advanced.Modifiers, "+")) + "+" + key
		}
		switch key {
		case "escape", "esc", "tab", "shift+tab", "arrowup", "arrowdown", "arrowleft", "arrowright",
			"pageup", "pagedown", "home", "end", "shift+0", "shift+1", "shift+2", "ctrl+0", "ctrl+=", "ctrl+-", "ctrl+plus", "ctrl+minus":
			return false
		}
		// A bare letter or digit is a tool or view shortcut in editors; the
		// edit it prepares (a drag, inserted text) is what counts.
		if len([]rune(key)) == 1 {
			return false
		}
	}
	return true
}

func browserCallKey(action string, params BrowserParams) string {
	data, _ := json.Marshal(params)
	sum := sha256.Sum256(append([]byte(action+"\x00"), data...))
	return hex.EncodeToString(sum[:12])
}

// browserFailureKey distinguishes a ref that has been freshly observed as
// another target from a repeat of the same failed input. Observation IDs
// alone never change the key.
func browserFailureKey(sessionID, action string, params BrowserParams) string {
	hasRef := params.Ref != ""
	for _, field := range params.Advanced.Fields {
		hasRef = hasRef || field.Ref != ""
	}
	if !hasRef {
		return browserCallKey(action, params)
	}
	refs := map[string]browser.RefIdentity{}
	var exec browser.ExecutionIdentity
	browserObservations.mu.Lock()
	if grant := browserObservations.grants[sessionID]; grant != nil {
		exec = grant.exec
		if params.Ref != "" {
			refs[params.Ref] = grant.refs[params.Ref]
		}
		for _, field := range params.Advanced.Fields {
			if field.Ref != "" {
				refs[field.Ref] = grant.refs[field.Ref]
			}
		}
	}
	browserObservations.mu.Unlock()
	data, _ := json.Marshal(struct {
		Call string
		Exec browser.ExecutionIdentity
		Refs map[string]browser.RefIdentity
	}{browserCallKey(action, params), exec, refs})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

// guardRepeat refuses a mutating call identical to one that already failed
// twice without successful input in between. Merely reading or capturing
// the page does not establish recovery from the failed operation.
func (l *browserLedger) guardRepeat(sessionID, action string, params BrowserParams) error {
	if !browserMutating(action) {
		return nil
	}
	key := browserFailureKey(sessionID, action, params)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failStreak >= 2 && l.lastFailKey == key {
		l.metrics.BlockedRepeats++
		return fmt.Errorf("repeated_failure: this exact %s call already failed %d times without successful input in between; nothing was sent. Observation alone does not unlock it. Inspect the current state and choose a different target, commit or route", action, l.failStreak)
	}
	return nil
}

// browserErrorCode classifies a failed result into one code and recovery
// shared by every browser path, so the same failure means the same thing
// whether it came from a direct call or a workflow step.
func browserErrorCode(text string) (code, recovery string) {
	lower := strings.ToLower(text)
	switch {
	case strings.HasPrefix(lower, "missing_target:"):
		return "missing_target", "Nothing was sent. Give a current ref, or use the semantic action the error names (semantic_click, semantic_type, set_field) or insert_text for an editor that already has focus."
	case strings.HasPrefix(lower, "unsupported_selector:") || strings.Contains(lower, "is not a valid selector"):
		return "unsupported_selector", "Nothing was sent. selector is CSS only; target visible text with semantic_click/find and advanced.name plus match."
	case strings.HasPrefix(lower, "focus_not_editable:") || strings.HasPrefix(lower, "focus_unknown:"):
		return "focus_not_editable", "Nothing was sent. Focus the text field or enter the editor's text-edit mode, confirm it, then insert the text."
	case strings.HasPrefix(lower, "invalid_arguments:") || strings.HasPrefix(lower, "unknown_field:") || strings.HasPrefix(lower, "conflicting_fields:") ||
		strings.HasPrefix(lower, "missing_value:") || strings.HasPrefix(lower, "invalid_field_request:") || strings.HasPrefix(lower, "invalid_input:"):
		return "invalid_arguments", "Nothing was sent. Correct the named field (see the tool contract's next_step and example) and call again."
	case strings.Contains(lower, "repeated_failure"):
		return "repeated_failure", "Inspect the page and choose a different target, commit or route. Reading or capturing the page alone does not unlock identical failed input."
	case strings.Contains(lower, "unsupported_key") || strings.Contains(lower, "unsupported key"):
		return "unsupported_key", "Send letters, digits, punctuation, named keys or chords like ctrl+a with action key or key_combo; no other key names exist."
	case strings.Contains(lower, "stale_viewport"):
		return "stale_viewport", "Take a new viewport screenshot and aim from its viewport_id."
	case strings.Contains(lower, "tab_closed"):
		return "tab_closed", "List tabs, then tab_return or tab_select an open tab. Do not restart the browser."
	case strings.Contains(lower, "browser was closed or disconnected"):
		return "browser_disconnected", "Call close once, then navigate to the task URL if the task is still active."
	case strings.Contains(lower, "wrong_tab") || strings.Contains(lower, "wrong_browser_context") || strings.Contains(lower, "unobserved_tab"):
		return "wrong_tab", "List tabs and select the intended tab; refs and viewport ids from another tab are invalid."
	case strings.Contains(lower, "stale_ref"):
		return "stale_ref", "Observe the page and target by a ref from that observation, or by role/name/label."
	case strings.Contains(lower, "frame_unreachable"):
		return "frame_unreachable", "The frame belongs to another site and runs in its own process; open its page in its own tab if the task needs it."
	case strings.Contains(lower, "stale_document") || strings.Contains(lower, "stale_frame"):
		return "stale_document", "The page navigated; observe the new document and resolve targets again."
	case strings.HasPrefix(lower, "target_not_found:") || strings.Contains(lower, "click failed: target_not_found"):
		return "target_not_found", "Nothing was sent. The target is not on the page: observe or find it again and use its current name or ref; do not retry the same target unchanged."
	case strings.Contains(lower, "covered by") || strings.Contains(lower, "target_not_actionable"):
		return "target_not_actionable", "Another element covers the control or it is disabled: observe what the error names (close that overlay or wait for it) before retrying."
	case strings.Contains(lower, "condition_timeout"):
		return "condition_timeout", "Read the last observed state in the error; wait for a condition that matches it or change the route. Do not repeat the same wait unchanged."
	case strings.Contains(lower, "pending dialog") || strings.Contains(lower, "dialog_timeout") || strings.Contains(lower, "dialog_mismatch") || strings.Contains(lower, "javascript dialog"):
		return "dialog_blocking", "Inspect with dialog_wait and answer with dialog_handle before other actions."
	case strings.Contains(lower, "input_cleanup_failed") || strings.Contains(lower, "release could not be confirmed"):
		return "input_cleanup_failed", "Held input is pending release; observe and wait for recovery. Never replay the partial gesture."
	case strings.Contains(lower, "value_not_applied"):
		return "value_not_applied", "The field kept another value. Observe it; try a different commit or route instead of repeating the same input."
	case strings.HasPrefix(lower, "menu_trigger_missing:"):
		return "menu_trigger_missing", "Nothing was sent. Inspect the control's visible menu opener and use a fresh semantic target or viewport screenshot; do not keep clicking the number field."
	case strings.Contains(lower, "outcome_unknown"):
		return "outcome_unknown", "Observe whether the change happened before any retry, so it is not applied twice."
	case strings.Contains(lower, "duplicate_submit"):
		return "duplicate_submit_risk", "Check for a confirmation or the created record first; resubmit only with allow_resubmit after verifying the first submission failed."
	case strings.Contains(lower, "clipboard"):
		return "clipboard_blocked", "Use insert_text or set_field for text. Ask the user before touching their clipboard."
	case strings.Contains(lower, "eval_timeout"):
		return "eval_timeout", "The script awaited something that never settled; observe the page instead of re-running it."
	case strings.Contains(lower, "password_field"):
		return "password_field", "Passwords never go through field, type or eval actions. Follow next_step: vault_fill with a saved handle, or credential_request so the user types it on their paired phone. Never ask for a password in chat."
	case strings.Contains(lower, "within_not") || strings.Contains(lower, "target_resolution_failed"):
		return "target_resolution_failed", "Refine within/scope/role/name from a fresh observation."
	case strings.Contains(lower, "undo_unsafe"):
		return "undo_unsafe", "Someone changed the value after Atlas set it; leave it, or ask the user."
	}
	return interaction.Failure(text)
}

// dispatchState decides what is known about delivery for a finished call.
func dispatchState(action string, resp fantasy.ToolResponse, code string) string {
	if !browserMutating(action) {
		return "none"
	}
	if !resp.IsError {
		return "sent"
	}
	if strings.Contains(resp.Content, `"input_sent":false`) && strings.Contains(resp.Content, `"effect":"none"`) {
		// A tool contract rejection says so itself.
		return "none"
	}
	switch code {
	case "missing_target", "unsupported_selector", "focus_not_editable", "invalid_arguments":
		// Refused by the argument contract before any input.
		return "none"
	case "timeout", "outcome_unknown", "driver_error", "input_cleanup_failed", "browser_disconnected", "tab_closed":
		return "unknown"
	case "condition_timeout", "target_not_actionable":
		// Resolution and waits refuse before dispatch; a click that timed out
		// after sending says so in its own content.
		if strings.Contains(resp.Content, "action_sent\":true") {
			return "sent"
		}
		return "none"
	case "value_not_applied", "duplicate_submit_risk":
		return "sent"
	}
	if strings.Contains(resp.Content, `"dispatch":"sent"`) || strings.Contains(resp.Content, "action_sent\":true") {
		return "sent"
	}
	return "none"
}

// verificationState reports whether the call itself established an effect.
func verificationState(action string, resp fantasy.ToolResponse) string {
	if resp.IsError {
		if strings.Contains(resp.Content, "value_not_applied") || strings.Contains(resp.Content, "gate_failed") || strings.Contains(resp.Content, "condition_timeout") {
			return "failed"
		}
		return "none"
	}
	switch action {
	case "assert", "wait_for", "popup_wait", "dialog_wait", "download_wait":
		return "structural"
	case "set_field", "select_option", "fill_form", "undo":
		if strings.Contains(resp.Content, `"verification":"verified"`) || strings.Contains(resp.Content, `"applied":`) {
			return "structural"
		}
		return "differs"
	case "read_field", "observe", "snapshot", "find", "text", "html", "url", "collect", "research", "tabs":
		return "observed"
	case "screenshot", "capture_region":
		return "visual"
	}
	return "none"
}

// recordBrowserOp counts a finished call, assigns its op id and attaches
// the execution record to the response the model reads.
func recordBrowserOp(sessionID, action string, params BrowserParams, resp fantasy.ToolResponse, started time.Time, exec browser.ExecutionIdentity) (fantasy.ToolResponse, BrowserOp) {
	tab := exec.TabID
	l := ledgerFor(sessionID)
	code, recovery := "", ""
	if resp.IsError {
		code, recovery = browserErrorCode(resp.Content)
	}
	failureKey := browserFailureKey(sessionID, action, params)
	l.mu.Lock()
	l.seq++
	op := BrowserOp{ID: browserOpID(l.seq), Seq: l.seq, Action: action, Observation: browserObservations.current(sessionID), DurationMS: time.Since(started).Milliseconds(), ErrorCode: code, Recovery: recovery, OK: !resp.IsError, Epoch: browserProcessEpoch, At: time.Now(), Key: browserCallKey(action, params), Browser: exec.Instance, Document: exec.Document}
	// The op record shows the tab only when it changed; the stored op keeps
	// it always, for evidence.
	showTab := tab != "" && tab != l.lastTab
	op.Tab = tab
	if tab != "" {
		l.lastTab = tab
	}
	op.Dispatch = dispatchState(action, resp, code)
	op.Verification = verificationState(action, resp)
	if strings.Contains(resp.Content, `"cleanup_status":"released"`) {
		op.Cleanup = "released"
	} else if strings.Contains(resp.Content, `"cleanup_status":"pending"`) {
		op.Cleanup = "pending"
	}
	op.Target = describeTarget(params)
	if op.Target == "" {
		op.Target = strings.TrimSpace(params.Advanced.Role + " " + params.Advanced.Name + " " + params.Advanced.Label)
	}
	op.TargetKeys, op.ChangedKeys, op.ChangeUncertain = fieldOperationEvidence(action, params, resp)
	op.Delivery = browserDeliveryAction(action, params) && browserVerifiedDelivery(action, resp)
	if !resp.IsError && browserVerifyingActions[action] {
		op.ObservedRaw = truncateRunes(resp.Content, maxObservedRunes)
		op.Observed = strings.ToLower(op.ObservedRaw)
	}
	m := &l.metrics
	m.Calls++
	m.ByAction[action]++
	m.ToolMS += op.DurationMS
	if action == "wait_for" || action == "popup_wait" || action == "dialog_wait" || action == "download_wait" {
		m.WaitMS += op.DurationMS
	}
	if resp.Type == "image" {
		m.Screenshots++
	}
	if l.lastFailed {
		m.Recoveries++
	}
	if !resp.IsError {
		if l.lastFailed {
			m.Recovered++
		} else {
			m.FirstTry++
		}
	}
	if resp.IsError {
		m.Errors++
		m.ErrorCodes[code]++
		if browserMutating(action) {
			key := failureKey
			if l.lastFailKey == key {
				l.failStreak++
			} else {
				l.failStreak = 1
			}
			l.lastFailKey = key
		}
		l.lastFailed = true
	} else {
		l.lastFailed = false
		if browserMutating(action) {
			l.failStreak, l.lastFailKey = 0, ""
		}
	}
	l.ops = append(l.ops, op)
	if len(l.ops) > 400 {
		l.ops = l.ops[len(l.ops)-400:]
	}
	l.mu.Unlock()
	if !browserRawDataActions[action] || resp.IsError {
		shown := op
		if !showTab {
			shown.Tab = ""
		}
		resp = attachBrowserOp(resp, shown)
	}
	l.mu.Lock()
	m.OutputChars += len(resp.Content)
	l.mu.Unlock()
	return resp, op
}

// browserRawDataActions return page data that callers consume verbatim (a
// URL, text, markup, a script value). Their successful results stay
// unmodified; the call is still counted and recorded.
var browserRawDataActions = map[string]bool{"url": true, "text": true, "html": true, "eval": true, "console": true, "images": true, "cdp": true}

// attachBrowserOp adds the execution record to the result: as an "op" key
// in a JSON object result (bindings and gates keep working), otherwise as
// one trailing line.
func attachBrowserOp(resp fantasy.ToolResponse, op BrowserOp) fantasy.ToolResponse {
	data, _ := json.Marshal(op)
	trimmed := strings.TrimSpace(resp.Content)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var object map[string]json.RawMessage
		if json.Unmarshal([]byte(trimmed), &object) == nil {
			object["op"] = data
			if encoded, err := json.Marshal(object); err == nil {
				resp.Content = string(encoded)
				return resp
			}
		}
	}
	if resp.Content != "" {
		resp.Content += "\n"
	}
	resp.Content += "[op] " + string(data)
	return resp
}

func (l *browserLedger) recordEdit(edit fieldEdit) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.edits = append(l.edits, edit)
	if len(l.edits) > 50 {
		l.edits = l.edits[len(l.edits)-50:]
	}
}

func (l *browserLedger) lastEdit() (fieldEdit, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.edits) == 0 {
		return fieldEdit{}, false
	}
	return l.edits[len(l.edits)-1], true
}

func (l *browserLedger) popEdit() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.edits) > 0 {
		l.edits = l.edits[:len(l.edits)-1]
	}
}

// claimSubmission records a submission fingerprint, refusing an identical
// one already sent in this session unless explicitly allowed.
func (l *browserLedger) claimSubmission(fingerprint string, allow bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if at, ok := l.submissions[fingerprint]; ok && !allow {
		return fmt.Errorf("duplicate_submit_risk: an identical submission was sent at %s in this session; nothing was clicked. Verify whether it was accepted before resubmitting", at.Format("15:04:05"))
	}
	l.submissions[fingerprint] = time.Now()
	return nil
}

// resetBrowserLedger drops per-session state; tests use it for isolation.
func resetBrowserLedger(sessionID string) {
	browserLedgersMu.Lock()
	delete(browserLedgers, sessionID)
	browserLedgersMu.Unlock()
	browserObservations.invalidate(sessionID)
}

// withBrowserFailure attaches the same error code and recovery the op record
// carries, so metadata and content never disagree about a failure.
func withBrowserFailure(response fantasy.ToolResponse) fantasy.ToolResponse {
	if !response.IsError {
		return response
	}
	code, recovery := browserErrorCode(response.Content)
	// Merge, never replace: outcome and credential metadata must survive.
	return withMergedMetadata(response, struct {
		Code              string `json:"error_code"`
		Recovery          string `json:"recovery"`
		VerifyBeforeRetry bool   `json:"verify_before_retry"`
	}{code, recovery, true})
}
