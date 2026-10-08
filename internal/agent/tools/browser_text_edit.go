package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// canvasTextEdit is the text-edit session of a canvas editor: opened by the
// click that put focus into the editor, closed by escape, another target or
// navigation. A canvas application's editor can keep focus after it left
// text editing, so text is accepted only while a session opened in this tab
// and document is still open, and only for the editor that click focused.
type canvasTextEdit struct {
	tab, doc string
	editor   string
	initial  string
	inserted string
	openedAt time.Time
}

func (l *browserLedger) textEditSession() *canvasTextEdit {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.textEdit == nil {
		return nil
	}
	edit := *l.textEdit
	return &edit
}

func (l *browserLedger) setTextEdit(edit *canvasTextEdit) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.textEdit = edit
}

// browserTextEditCloses are actions after which the focused canvas editor no
// longer belongs to the text the session opened.
var browserTextEditCloses = map[string]bool{
	"click": true, "semantic_click": true, "semantic_type": true, "type": true, "set_field": true, "select_option": true,
	"fill_form": true, "submit": true, "navigate": true, "back": true, "forward": true, "tab_new": true, "tab_select": true,
	"tab_close": true, "tab_return": true, browser.InputPointerDrag: true, "undo": true,
}

// browserTextEditOpeners can put focus into a canvas editor.
var browserTextEditOpeners = map[string]bool{browser.InputPointerClick: true, "click": true, "semantic_click": true}

// focusAfterInput reads focus once an action settled; editors often move
// focus to their text editor a moment after the click that opened it. One
// bounded second look keeps the cost of ordinary canvas clicks small.
func focusAfterInput(ctx context.Context, reporter browser.FocusSession, action string) (browser.FocusInfo, error) {
	info, err := reporter.Focus(ctx)
	if err != nil || info.Editable || action != browser.InputPointerClick {
		return info, err
	}
	select {
	case <-ctx.Done():
		return info, ctx.Err()
	case <-time.After(150 * time.Millisecond):
	}
	return reporter.Focus(ctx)
}

// trackCanvasTextEdit updates the session after a successful action.
func trackCanvasTextEdit(ctx context.Context, sess browser.Session, stateID, action string, params BrowserParams, exec browser.ExecutionIdentity) {
	ledger := ledgerFor(stateID)
	key := strings.ToLower(firstNonEmpty(params.Key, params.Advanced.Key))
	switch {
	case browserTextEditOpeners[action]:
		reporter, ok := sess.(browser.FocusSession)
		if !ok {
			ledger.setTextEdit(nil)
			return
		}
		info, err := focusAfterInput(ctx, reporter, action)
		if err != nil || !info.Editable || !info.CanvasEditor {
			ledger.setTextEdit(nil)
			return
		}
		ledger.setTextEdit(&canvasTextEdit{tab: exec.TabID, doc: exec.Document, editor: info.Key, initial: info.Text, openedAt: time.Now()})
	case action == "key" && (key == "escape" || key == "esc" || key == "tab" || key == "shift+tab"),
		action == browser.InputKeyCombo && (key == "escape" || key == "esc"),
		browserTextEditCloses[action]:
		ledger.setTextEdit(nil)
	}
}

// singleCharacterKey reports a key press that types a character into a
// focused editor instead of acting as an application shortcut.
func singleCharacterKey(action string, params BrowserParams) (string, bool) {
	key := firstNonEmpty(params.Key, params.Advanced.Key)
	if action == browser.InputKeyCombo {
		for _, m := range params.Advanced.Modifiers {
			if !strings.EqualFold(m, "shift") {
				return "", false
			}
		}
	} else if action != "key" {
		return "", false
	}
	if utf8.RuneCountInString(key) != 1 || key == " " {
		return "", false
	}
	return key, true
}

// keyWouldType refuses a single-character shortcut while focus is in a text
// editor: the editor would receive it as text. In session 14b5c83a a "t"
// meant to pick the text tool was typed into the previous text layer.
func keyWouldType(ctx context.Context, sess browser.Session, action string, params BrowserParams) error {
	key, single := singleCharacterKey(action, params)
	if !single {
		return nil
	}
	reporter, ok := sess.(browser.FocusSession)
	if !ok {
		return nil
	}
	info, err := reporter.Focus(ctx)
	if err != nil || !info.Editable {
		return nil
	}
	data, _ := json.Marshal(info)
	return computer.NewContractError("key_would_type", "key",
		fmt.Sprintf("key %q would be typed into the focused text editor %s, not used as a shortcut. No input sent", key, data),
		"To pick a tool, click its toolbar button (semantic_click) or leave text editing first (key escape, then confirm focus left the editor). To type the character, use insert_text.",
		`{"action":"semantic_click","advanced":{"role":"button","name":"Text"}}`)
}

// canvasInsertGuard decides whether insert_text may go to a canvas editor.
// It requires an open session for the focused editor in this tab and
// document, and refuses to append to existing text unless mode is append.
func canvasInsertGuard(stateID string, info browser.FocusInfo, exec browser.ExecutionIdentity, mode string) *computer.ContractError {
	if !info.CanvasEditor {
		return nil
	}
	edit := ledgerFor(stateID).textEditSession()
	example := `{"action":"pointer_click","advanced":{"viewport_id":"<viewport_id from screenshot>","x":200,"y":160}}`
	switch {
	case edit == nil:
		return computer.NewContractError("text_edit_not_open", "focus",
			"the focused element is the canvas application's text editor, but no text-edit session is open: it can stay focused after editing ended, so this text could land in the previously edited layer. No input sent",
			"Select the text tool (its toolbar button), click the canvas point for new text (or double-click existing text to edit it) with pointer_click, then insert_text. Escape, other clicks and navigation end the session.", example)
	case edit.tab != exec.TabID || edit.doc != exec.Document:
		return computer.NewContractError("text_edit_not_open", "focus", "the text-edit session was opened in another tab or document. No input sent", "Open a new session in this document: click the canvas with the text tool, then insert_text.", example)
	case edit.editor != info.Key:
		return computer.NewContractError("text_edit_not_open", "focus", "focus moved to another editor since the click that opened text editing. No input sent", "Click the canvas again to open the intended text, then insert_text.", example)
	case canvasTextHasContent(edit.initial) && edit.inserted == "" && mode != "append":
		return computer.NewContractError("text_editor_not_empty", "advanced.mode",
			fmt.Sprintf("the opened editor already holds %q, so this text would be added to an existing text object, not create a new one. No input sent", edit.initial),
			"To add to that text, send advanced.mode append. To create new text, press escape, select the text tool, click an empty canvas point and insert again.",
			`{"action":"insert_text","text":"Remote Control","advanced":{"mode":"append"}}`)
	}
	return nil
}

// canvasTextHasContent ignores directional wrappers that canvas editors
// put around an empty value. Spaces and joining characters remain content;
// stripping them could mistake a real text object for a new empty editor.
func canvasTextHasContent(text string) bool {
	return strings.Trim(text, "\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069") != ""
}

// recordCanvasInsert keeps the session's expected text after an insertion
// and reports the editor before and after, so the model sees whether the
// text went where it intended.
func recordCanvasInsert(ctx context.Context, sess browser.Session, stateID string, before browser.FocusInfo, text string) map[string]any {
	if !before.CanvasEditor {
		return nil
	}
	ledger := ledgerFor(stateID)
	if edit := ledger.textEditSession(); edit != nil {
		edit.inserted += text
		ledger.setTextEdit(edit)
	}
	report := map[string]any{"editor_text_before": before.Text}
	if reporter, ok := sess.(browser.FocusSession); ok {
		if after, err := reporter.Focus(ctx); err == nil && after.Key == before.Key {
			report["editor_text_after"] = after.Text
		}
	}
	return report
}

// withCanvasInsertReport adds the editor readback to an insert_text result.
func withCanvasInsertReport(resp fantasy.ToolResponse, report map[string]any) fantasy.ToolResponse {
	if report == nil || resp.IsError {
		return resp
	}
	var object map[string]any
	if json.Unmarshal([]byte(resp.Content), &object) != nil {
		return resp
	}
	object["canvas_text"] = report
	object["next"] = "Text reached the canvas editor; editor_text_after shows what it holds now. Confirm the text object on the canvas or in the layer list before relying on it."
	data, _ := json.Marshal(object)
	resp.Content = string(data)
	return resp
}
