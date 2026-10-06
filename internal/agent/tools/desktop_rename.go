package tools

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// DesktopRenameParams names one observed item, preserving Explorer's display policy.
type DesktopRenameParams struct {
	OldName string `json:"old_name" description:"Exact observed Explorer ListItem name, including extension only if displayed."`
	NewName string `json:"new_name" description:"Exact replacement name using the same extension visibility policy. Never guess or remove a hidden extension."`
}

func desktopRenameName(s string) bool {
	if s == "" || !utf8.ValidString(s) || len(s) > 1020 || strings.TrimRight(s, " .") != s || strings.ContainsAny(s, `<>:"/\|?*`) {
		return false
	}
	units := 0
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
		units++
		if r > 0xffff {
			units++
		}
	}
	base := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" {
		return false
	}
	runes := []rune(base)
	if len(runes) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && strings.ContainsRune("123456789¹²³", runes[3]) {
		return false
	}
	return units <= 255
}

func desktopRenameEditor(e *desktopElement, item desktopElement, expected string, id string) bool {
	if e == nil || e.ID == "" || e.ID == item.ID || !e.KeyboardFocused || !e.Enabled || e.Offscreen || e.Password || e.ProcessID != item.ProcessID || e.Width <= 0 || e.Height <= 0 || item.Width <= 0 || item.Height <= 0 {
		return false
	}
	if e.Role != "ControlType.Edit" && e.Role != "ControlType.Pane" {
		return false
	}
	for _, n := range []float64{e.X, e.Y, e.Width, e.Height, item.X, item.Y, item.Width, item.Height} {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return false
		}
	}
	// The focused inline editor must occupy this exact selected item's bounds.
	if e.X < item.X-2 || e.Y < item.Y-2 || e.X+e.Width > item.X+item.Width+2 || e.Y+e.Height > item.Y+item.Height+2 || id != "" && e.ID != id {
		return false
	}
	if e.ValueAvailable {
		return !e.ValueTruncated && e.Value == expected
	}
	return e.Name == expected
}

func runDesktopRename(ctx context.Context, p DesktopWorkflowParams, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	if p.Rename == nil || p.WindowID == "" || p.Application != "" || !reflect.DeepEqual(p.Input, ComputerParams{}) || len(p.Inputs) != 0 || len(p.Steps) != 0 || len(p.AdaptiveSteps) != 0 || p.Transition != nil || p.Flow != nil || p.Result != "" || p.WaitFor != (computer.AutomationRequest{}) || p.WaitMS != 0 || p.MaxElements != 0 || (p.Observation != "" && p.Observation != "auto" && p.Observation != "semantic") {
		return fantasy.NewTextErrorResponse("rename requires only window_id and rename names"), nil
	}
	if !desktopRenameName(p.Rename.OldName) || !desktopRenameName(p.Rename.NewName) {
		return fantasy.NewTextErrorResponse("invalid_name: rename requires safe exact leaf names"), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	child := desktopRecipeChild(parent, invoke)
	windows, r, err := desktopFlowWindows(ctx, child)
	if desktopRecipeStopped(r, err) {
		return r, err
	}
	var window desktopWindowInfo
	for _, w := range windows {
		if w.ID == p.WindowID && desktopApplicationWindowMatches(w, "File Explorer") && w.Foreground && !w.Minimized && w.ProcessID > 0 {
			window = w
		}
	}
	if window.ID == "" {
		return fantasy.NewTextErrorResponse("wrong_window: rename requires the observed foreground Explorer folder window"), nil
	}
	selector := computer.AutomationRequest{WindowID: window.ID, Name: p.Rename.OldName, Role: "ControlType.ListItem"}
	item, r, err := desktopFlowTarget(ctx, selector, child)
	if desktopRecipeStopped(r, err) {
		if err == nil && r.IsError && !r.StopTurn && strings.HasPrefix(r.Content, "target_missing:") {
			q := selector
			q.Name = ""
			observed, diagnostic, diagnosticErr := desktopFlowFind(ctx, q, child)
			if desktopRecipeStopped(diagnostic, diagnosticErr) {
				return diagnostic, diagnosticErr
			}
			names := []string{}
			for _, candidate := range observed.Elements {
				if !candidate.Password && !candidate.Offscreen && candidate.Enabled && candidate.ProcessID == int(window.ProcessID) && len(names) < 20 {
					names = append(names, candidate.Name)
				}
			}
			data, _ := json.Marshal(names)
			r.Content = "target_missing: exact displayed item name was not found. Explorer may hide extensions; use fresh observed display names for old_name/new_name. Do not infer the real extension or replay input. No rename input sent. Visible ListItem names: " + string(data)
		}
		return r, err
	}
	if item.Offscreen || !item.Enabled || item.ProcessID != int(window.ProcessID) {
		return fantasy.NewTextErrorResponse("target_not_actionable: resolve the visible Explorer item before renaming"), nil
	}
	if p.Rename.OldName != p.Rename.NewName {
		q := selector
		q.Name = p.Rename.NewName
		o, result, callErr := desktopFlowFind(ctx, q, child)
		if desktopRecipeStopped(result, callErr) {
			return result, callErr
		}
		if len(o.Elements) != 0 {
			return fantasy.NewTextErrorResponse("name_conflict: replacement item already exists; no rename input sent"), nil
		}
		if r, err = desktopFlowIdentity(ctx, window, false, child); desktopRecipeStopped(r, err) {
			return r, err
		}
		selector.ElementID = item.ID
		r, err = child(ctx, ComputerParams{Action: "select", Automation: selector})
		if desktopRecipeStopped(r, err) {
			return r, err
		}
		var selection struct {
			Result struct {
				Selected bool   `json:"selected"`
				Focused  bool   `json:"keyboard_focused"`
				Count    int    `json:"selection_count"`
				ID       string `json:"element_id"`
				WindowID string `json:"window_id"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(r.Content), &selection) != nil || !selection.Result.Selected || !selection.Result.Focused || selection.Result.Count != 1 || selection.Result.ID != item.ID || selection.Result.WindowID != window.ID {
			return fantasy.NewTextErrorResponse("selection_unverified: F2 was not sent"), nil
		}
		key := func(action, name, modifiers, text string) (fantasy.ToolResponse, error) {
			return child(ctx, ComputerParams{Action: action, Key: name, Modifiers: modifiers, Text: text, Automation: computer.AutomationRequest{WindowID: window.ID}})
		}
		if r, err = key("key", "f2", "", ""); desktopRecipeStopped(r, err) {
			return r, err
		}
		readEditor := func(expected, editorID string) (*desktopElement, fantasy.ToolResponse, error) {
			r, err := child(ctx, ComputerParams{Action: "inspect", Automation: computer.AutomationRequest{WindowID: window.ID, MaxElements: 1}})
			if desktopRecipeStopped(r, err) {
				return nil, r, err
			}
			var data struct {
				Result struct {
					WindowID string          `json:"window_id"`
					Focused  *desktopElement `json:"focused_element"`
				} `json:"result"`
			}
			if json.Unmarshal([]byte(r.Content), &data) != nil || data.Result.WindowID != window.ID || !desktopRenameEditor(data.Result.Focused, *item, expected, editorID) {
				return nil, fantasy.NewTextErrorResponse("rename_editor_unverified: inspect once; no dependent input or automatic replay"), nil
			}
			return data.Result.Focused, r, nil
		}
		editor, result, callErr := readEditor(p.Rename.OldName, "")
		if desktopRecipeStopped(result, callErr) {
			return result, callErr
		}
		if r, err = key("hotkey", "a", "ctrl", ""); desktopRecipeStopped(r, err) {
			return r, err
		}
		if r, err = key("type", "", "", p.Rename.NewName); desktopRecipeStopped(r, err) {
			return r, err
		}
		if _, r, err = readEditor(p.Rename.NewName, editor.ID); desktopRecipeStopped(r, err) {
			return r, err
		}
		if r, err = key("key", "enter", "", ""); desktopRecipeStopped(r, err) {
			return r, err
		}
		r, err = child(ctx, ComputerParams{Action: "inspect", Automation: computer.AutomationRequest{WindowID: window.ID, MaxElements: 1}})
		if desktopRecipeStopped(r, err) {
			return r, err
		}
		var committed struct {
			Result struct {
				WindowID string          `json:"window_id"`
				Focused  *desktopElement `json:"focused_element"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(r.Content), &committed) != nil || committed.Result.WindowID != window.ID || committed.Result.Focused == nil || committed.Result.Focused.ID == editor.ID || !committed.Result.Focused.KeyboardFocused {
			return fantasy.NewTextErrorResponse("rename_unverified: editor closure not confirmed; do not replay Enter"), nil
		}
		q = computer.AutomationRequest{WindowID: window.ID, Name: p.Rename.OldName, Role: "ControlType.ListItem", Condition: "hidden", WaitMS: 3000}
		r, err = child(ctx, ComputerParams{Action: "assert", Automation: q})
		if desktopRecipeStopped(r, err) {
			return r, err
		}
		if !desktopCheckpointReadback(r.Content, q) {
			return fantasy.NewTextErrorResponse("rename_unverified: original item remains visible"), nil
		}
	}
	q := computer.AutomationRequest{WindowID: window.ID, Name: p.Rename.NewName, Role: "ControlType.ListItem", Condition: "text", Expected: p.Rename.NewName, WaitMS: 3000}
	r, err = child(ctx, ComputerParams{Action: "assert", Automation: q})
	if desktopRecipeStopped(r, err) {
		return r, err
	}
	if !desktopCheckpointReadback(r.Content, q) {
		return fantasy.NewTextErrorResponse("rename_unverified: missing exact final name readback"), nil
	}
	if r, err = desktopFlowIdentity(ctx, window, false, child); desktopRecipeStopped(r, err) {
		return r, err
	}
	data, _ := json.Marshal(map[string]any{"workflow": map[string]any{"mode": "rename", "window_id": window.ID, "old_name": p.Rename.OldName, "actual_name": p.Rename.NewName, "condition_verified": true, "task_completion_requires_verification": true}})
	return fantasy.NewTextResponse(string(data)), nil
}
