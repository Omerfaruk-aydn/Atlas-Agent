package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// browserExtendedActions run through runExtendedBrowser. They share the
// browser tool's permission, hook, ownership, trace and op-record path.
var browserExtendedActions = map[string]bool{
	"observe": true, "set_field": true, "read_field": true, "select_option": true, "fill_form": true,
	"submit": true, "collect": true, "research": true, "health": true, "undo": true, "knowledge": true,
}

// browserTaskActions only touch the completion contract, never the page.
var browserTaskActions = map[string]bool{"task_plan": true, "task_update": true, "task_status": true}

func browserJSONResponse(v any) fantasy.ToolResponse {
	data, _ := json.Marshal(v)
	return fantasy.NewTextResponse(string(data))
}

func browserJSONError(v any, err error) fantasy.ToolResponse {
	data, _ := json.Marshal(v)
	return fantasy.NewTextErrorResponse(err.Error() + "\n" + string(data))
}

// fieldRequest builds a field request from the advanced block, resolving a
// ref into its selector and text into the value.
func fieldRequest(action string, params BrowserParams) (browser.Request, error) {
	p := params.Advanced
	p.Action = action
	if p.Selector == "" && params.Ref != "" {
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return p, err
		}
		p.Selector = selector
	}
	if p.Selector == "" {
		p.Selector = params.Selector
	}
	if p.Value == "" {
		p.Value = params.Text
	}
	return p, nil
}

func runExtendedBrowser(ctx context.Context, sess browser.Session, action string, params BrowserParams, root, sessionID string) (fantasy.ToolResponse, error) {
	switch action {
	case "observe":
		mode := params.Advanced.Mode
		switch mode {
		case "":
			mode = "delta"
		case "full", "delta", "scope":
		default:
			return fantasy.NewTextErrorResponse("observe mode must be full, delta or scope"), nil
		}
		if mode == "scope" && params.Advanced.Scope == "" {
			return fantasy.NewTextErrorResponse("observe mode=scope requires advanced.scope (a CSS selector for one container)"), nil
		}
		obs, err := takeObservation(ctx, sess, sessionID, observeRequest{mode: mode, scope: params.Advanced.Scope, full: params.Full, cursor: params.Advanced.Cursor})
		if err != nil {
			return fantasy.NewTextErrorResponse("observe failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(obs.text), nil

	case "set_field", "read_field", "select_option":
		driver, ok := sess.(browser.FieldSession)
		if !ok {
			return fantasy.NewTextErrorResponse("Field commit and readback are unavailable in this browser driver"), nil
		}
		p, err := fieldRequest(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := browser.ValidateWorkflowRequest(p); err != nil {
			return contractResponse(computer.NewContractError("invalid_field_request", "advanced", err.Error()+". Nothing was sent", "Name the field with ref, selector, role/name or label (optionally within a container) and give value.", `{"action":"set_field","advanced":{"label":"Width","within":"Layout","value":"390"}}`)), nil
		}
		pageURL, _ := sess.URL()
		var result browser.FieldResult
		switch action {
		case "read_field":
			result, err = driver.ReadField(ctx, p)
		case "select_option":
			forms, ok := sess.(browser.FormSession)
			if !ok {
				return fantasy.NewTextErrorResponse("Option selection is unavailable in this browser driver"), nil
			}
			result, err = forms.SelectOption(ctx, p)
			if err == nil && result.Succeeded() {
				recordFieldEdit(sessionID, p, result, pageURL)
			}
		default:
			p.LearnedCommit = learnedFieldCommit(root, pageURL, "field", firstNonEmpty(p.Label, p.Name))
			result, err = driver.SetField(ctx, p)
			if len(result.CommitAttempts) > 0 && result.Status != browser.FieldTyped {
				label := firstNonEmpty(p.Label, p.Name, result.Name)
				attempts := result.CommitAttempts
				if result.Commit == "enter+blur" {
					attempts = []string{"enter+blur"}
				}
				learnFieldCommit(root, pageURL, "field", label, attempts, result.Succeeded(), browserOpID(ledgerSeqPeek(sessionID)+1))
			}
			if err == nil && result.Succeeded() {
				recordFieldEdit(sessionID, p, result, pageURL)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return fantasy.ToolResponse{}, ctx.Err()
			}
			return browserJSONError(result, err), nil
		}
		return browserJSONResponse(result), nil

	case "fill_form":
		forms, ok := sess.(browser.FormSession)
		if !ok {
			return fantasy.NewTextErrorResponse("Form filling is unavailable in this browser driver"), nil
		}
		p := params.Advanced
		p.Action = action
		for _, f := range p.Fields {
			if strings.Contains(strings.ToLower(f.Name+f.Label), "password") {
				return fantasy.NewTextErrorResponse("password_field: passwords are filled only with vault_fill"), nil
			}
		}
		result, err := forms.FillForm(ctx, p)
		if err != nil {
			if ctx.Err() != nil {
				return fantasy.ToolResponse{}, ctx.Err()
			}
			return browserJSONError(result, err), nil
		}
		return browserJSONResponse(result), nil

	case "submit":
		advanced, ok := sess.(browser.AdvancedSession)
		if !ok {
			return fantasy.NewTextErrorResponse("Submit is unavailable in this browser driver"), nil
		}
		p, err := fieldRequest(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := browser.ValidateWorkflowRequest(p); err != nil {
			return fantasy.NewTextErrorResponse(err.Error() + ". Nothing was clicked"), nil
		}
		fingerprint, err := submissionFingerprint(sess, p)
		if err != nil {
			return fantasy.NewTextErrorResponse("submit could not inspect the form before sending: " + err.Error()), nil
		}
		if err := ledgerFor(sessionID).claimSubmission(fingerprint, p.AllowResubmit); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		p.Action = "semantic_click"
		data, _, err := advanced.Advanced(ctx, p)
		if err != nil {
			return fantasy.NewTextErrorResponse("submit failed before or during the click: " + err.Error()), nil
		}
		var click map[string]any
		_ = json.Unmarshal(data, &click)
		return browserJSONResponse(map[string]any{
			"submitted": true, "dispatch": "sent", "accepted": "unknown", "target": click["target"],
			"next": "The submit control was clicked; that is not acceptance. Wait for the confirmation state (wait_for text/url/count) or a validation message before reporting success.",
		}), nil

	case "collect":
		driver, ok := sess.(browser.CollectSession)
		if !ok {
			return fantasy.NewTextErrorResponse("List collection is unavailable in this browser driver"), nil
		}
		p := params.Advanced
		p.Action = action
		result, err := driver.Collect(ctx, p)
		if err != nil {
			if ctx.Err() != nil {
				return fantasy.ToolResponse{}, ctx.Err()
			}
			return browserJSONError(result, err), nil
		}
		return browserJSONResponse(result), nil

	case "research":
		driver, ok := sess.(browser.ResearchSession)
		if !ok {
			return fantasy.NewTextErrorResponse("Background research tabs are unavailable in this browser driver"), nil
		}
		p := params.Advanced
		if len(p.URLs) == 0 && params.URL != "" {
			p.URLs = []string{params.URL}
		}
		sources, err := driver.Research(ctx, p)
		if err != nil && ctx.Err() != nil {
			return fantasy.ToolResponse{}, ctx.Err()
		}
		out := map[string]any{"sources": sources, "note": "Each source keeps its own requested and final URL. Background tabs were read-only and are closed; the controlled tab is unchanged."}
		if err != nil {
			return browserJSONError(out, err), nil
		}
		return browserJSONResponse(out), nil

	case "health":
		driver, ok := sess.(browser.HealthSession)
		if !ok {
			return fantasy.NewTextErrorResponse("Health probing is unavailable in this browser driver"), nil
		}
		h := driver.Health(ctx)
		metrics := BrowserSessionMetrics(sessionID)
		return browserJSONResponse(map[string]any{"health": h, "recent_error_codes": metrics.ErrorCodes}), nil

	case "undo":
		return undoLastFieldEdit(ctx, sess, sessionID)

	case "knowledge":
		pageURL, _ := sess.URL()
		report, err := browserKnowledgeReport(root, pageURL)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		return fantasy.NewTextResponse(report), nil
	}
	return fantasy.NewTextErrorResponse("unknown action: " + action), nil
}

func ledgerSeqPeek(sessionID string) int {
	l := ledgerFor(sessionID)
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq
}

// submissionFingerprint identifies a submission by page, control and the
// current field values (hashed, never stored in clear), so an identical
// resubmission is recognized without keeping what was entered.
func submissionFingerprint(sess browser.Session, p browser.Request) (string, error) {
	pageURL, err := sess.URL()
	if err != nil {
		return "", err
	}
	elements, err := sess.Snapshot(true)
	if err != nil {
		return "", err
	}
	var values []string
	for _, el := range elements {
		if el.Value != "" || el.Checked != "" {
			values = append(values, el.Context+"\x00"+el.Name+"\x00"+el.Value+"\x00"+el.Checked)
		}
	}
	sort.Strings(values)
	if u := knowledgeOrigin(pageURL); u != "" {
		pageURL = u + strings.SplitN(strings.TrimPrefix(pageURL, u), "?", 2)[0]
	}
	sum := sha256.Sum256([]byte(pageURL + "\x00" + p.Selector + p.Role + p.Name + p.Label + p.Within + "\x00" + strings.Join(values, "\x01")))
	return hex.EncodeToString(sum[:]), nil
}

// recordFieldEdit keeps what undo needs to restore a verified field change
// and to recognize the edited object later. The request is pinned to the
// frame and document the edit happened in.
func recordFieldEdit(sessionID string, p browser.Request, result browser.FieldResult, pageURL string) {
	target := p
	target.FrameID, target.DocumentID = result.FrameID, result.Identity.DocumentID
	ledgerFor(sessionID).recordEdit(fieldEdit{
		Target: target, Kind: result.Kind, Name: result.Name, Commit: result.Commit,
		Before: result.Before, Accepted: result.Accepted, AcceptedOption: result.AcceptedOption,
		Identity: result.Identity, Origin: knowledgeOrigin(pageURL), At: time.Now(),
	})
}

// undoMismatch explains an identity difference without revealing values.
var undoMismatch = map[string]string{
	"tab":       "another tab is selected than the one the edit was made in",
	"frame":     "the field now resolves in another frame",
	"document":  "the page or frame holds another document than when the edit was made (it navigated or reloaded)",
	"selection": "the selected object changed since the edit, so the panel now edits something else",
	"panel":     "the other fields of the panel changed since the edit, so it may now show another object",
}

// undoLastFieldEdit restores the value Atlas replaced in its most recent
// field edit. It acts only when the field is the same field of the same
// object in the same tab and document, and still holds exactly what Atlas
// set: a value someone changed afterwards is never overwritten. It never
// switches tabs, navigates or sends a generic undo shortcut. A refused or
// failed undo keeps the record.
func undoLastFieldEdit(ctx context.Context, sess browser.Session, sessionID string) (fantasy.ToolResponse, error) {
	driver, ok := sess.(browser.FieldSession)
	if !ok {
		return fantasy.NewTextErrorResponse("undo is unavailable in this browser driver"), nil
	}
	ledger := ledgerFor(sessionID)
	edit, ok := ledger.lastEdit()
	if !ok {
		return fantasy.NewTextErrorResponse("undo_unavailable: no field edit by Atlas is recorded in this session. Canvas gestures, clicks and submissions are not undone automatically"), nil
	}
	unsafe := func(reason string) (fantasy.ToolResponse, error) {
		return browserJSONError(map[string]any{"undone": false, "field": edit.Name, "record_kept": true}, errors.New("undo_unsafe: "+reason+"; nothing was changed. Return to the edited object yourself (or ask the user) before undoing")), nil
	}
	if tabs, ok := sess.(browser.TabIdentity); ok && edit.Identity.TabID != "" && tabs.ActiveTab() != edit.Identity.TabID {
		return unsafe(undoMismatch["tab"])
	}
	if edit.Identity.DocumentID == "" {
		return unsafe("the edit has no recorded document identity")
	}
	current, err := driver.ReadField(ctx, edit.Target)
	if err != nil {
		if ctx.Err() != nil {
			return fantasy.ToolResponse{}, ctx.Err()
		}
		code, _ := browserErrorCode(err.Error())
		if code == "stale_document" || code == "wrong_tab" || strings.Contains(err.Error(), "frame_unreachable") {
			return unsafe(undoMismatch["document"])
		}
		return browserJSONError(map[string]any{"undone": false, "field": edit.Name, "record_kept": true}, errors.New("undo_unavailable: the edited field cannot be resolved now ("+err.Error()+"); nothing was changed")), nil
	}
	if reason := edit.Identity.Mismatch(current.Identity); reason != "" {
		return unsafe(undoMismatch[reason])
	}
	if current.Before != edit.Accepted || (edit.Kind == "select" && current.AcceptedOption != edit.AcceptedOption) {
		return browserJSONError(map[string]any{"undone": false, "field": edit.Name, "atlas_set": edit.Accepted, "current": current.Before, "record_kept": true}, errors.New("undo_unsafe: the field changed after Atlas set it (someone else edited it); nothing was undone")), nil
	}
	restore := edit.Target
	restore.Value, restore.Text = edit.Before, ""
	var result browser.FieldResult
	if edit.Kind == "select" {
		forms, ok := sess.(browser.FormSession)
		if !ok {
			return fantasy.NewTextErrorResponse("undo_unavailable: option selection is unavailable in this browser driver"), nil
		}
		restore.Action = "select_option"
		result, err = forms.SelectOption(ctx, restore)
	} else {
		restore.Action, restore.Mode = "set_field", "replace"
		if edit.Before == "" {
			restore.Mode = "clear"
		}
		switch edit.Commit {
		case "enter", "tab", "blur":
			restore.Commit = edit.Commit
		default:
			restore.Commit = ""
		}
		result, err = driver.SetField(ctx, restore)
	}
	if err != nil {
		if ctx.Err() != nil {
			return fantasy.ToolResponse{}, ctx.Err()
		}
		return browserJSONError(map[string]any{"undone": false, "field": edit.Name, "restore": result, "record_kept": true}, fmt.Errorf("undo failed: %w", err)), nil
	}
	// The restore counts only when the readback shows the old value itself.
	if !result.Succeeded() || result.Accepted != edit.Before {
		return browserJSONError(map[string]any{"undone": false, "field": edit.Name, "restore": result, "record_kept": true}, fmt.Errorf("undo failed: value_not_applied: the field shows %q, not the previous %q", result.Accepted, edit.Before)), nil
	}
	ledger.popEdit()
	return browserJSONResponse(map[string]any{"undone": true, "field": edit.Name, "restored": result.Accepted, "replaced": edit.Accepted, "verification": result.Verification}), nil
}

var clipboardScript = regexp.MustCompile(`(?i)navigator\s*\.\s*clipboard|execCommand\s*\(\s*['"](copy|cut|paste)`)

// guardedEval runs eval with a bounded await and refuses clipboard access:
// the clipboard belongs to the user, and permission prompts can hang.
func guardedEval(ctx context.Context, sess browser.Session, params BrowserParams) (fantasy.ToolResponse, error) {
	if clipboardScript.MatchString(params.Script) {
		return contractResponse(computer.NewContractError("clipboard_blocked", "script", "clipboard_blocked: eval may not read or write the user's clipboard (it can wait on a permission prompt and overwrite what the user copied). Nothing ran", "Enter text with insert_text or set_field. If the task needs the clipboard, ask the user.", `{"action":"insert_text","text":"Heading"}`)), nil
	}
	timeout := 10 * time.Second
	if params.Advanced.TimeoutMS > 0 && params.Advanced.TimeoutMS <= 30000 {
		timeout = time.Duration(params.Advanced.TimeoutMS) * time.Millisecond
	}
	var result string
	var err error
	if driver, ok := sess.(browser.EvalSession); ok {
		result, err = driver.EvalContext(ctx, params.Script, timeout)
	} else {
		result, err = sess.Eval(params.Script)
	}
	if err != nil {
		if ctx.Err() != nil {
			return fantasy.ToolResponse{}, ctx.Err()
		}
		return fantasy.NewTextErrorResponse("eval failed: " + err.Error()), nil
	}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), BrowserResponseMetadata{Action: "eval"}), nil
}

// pressBrowserKey sends a key or chord through the typed input path when
// the driver has one, so held modifiers are released on failure. Names the
// legacy path cannot send ("f", "ctrl+a") work the same as key_combo.
func pressBrowserKey(ctx context.Context, sess browser.Session, raw string) error {
	key, modifiers, err := browser.NormalizeKey(raw)
	if err != nil {
		return err
	}
	if driver, ok := sess.(browser.KeySession); ok {
		_, err := driver.Input(ctx, browser.Request{Action: browser.InputKeyCombo, Key: key, Modifiers: modifiers})
		return err
	}
	if len(modifiers) == 0 {
		if _, ok := browser.ResolveKey(key); ok {
			return sess.PressKey(key)
		}
	}
	return browser.ErrUnsupportedKey{Key: raw}
}
