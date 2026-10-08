package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
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
	// ExpectedFullName is checked against the shell item, never the label.
	ExpectedFullName string `json:"expected_full_name,omitempty" description:"Optional exact file-system name with extension (e.g. sonuc.txt when Explorer shows sonuc). After the rename it is read from this Explorer window's shell view item, not from the display label; a mismatch or an unreadable name fails the check."`
	// OnConflict decides what happens when new_name already exists. Nothing
	// is ever deleted or overwritten.
	OnConflict string `json:"on_conflict,omitempty" enum:"fail,keep_both,use_existing_if_identical" description:"When an item named new_name already exists: fail (default) refuses before any input and reports both files' size and sha256; keep_both renames to the first free 'new_name (N)' and reports the actual name; use_existing_if_identical sends no input when the existing file's bytes equal the source's and reports the source as kept. No policy deletes or overwrites a file."`
}

// Rename conflict policies.
const (
	renameConflictFail        = "fail"
	renameConflictKeepBoth    = "keep_both"
	renameConflictUseExisting = "use_existing_if_identical"
)

// validateRenameConflictPolicy refuses unknown policies before any input.
// Replacing is deliberately not a policy: a rename never deletes a file.
func validateRenameConflictPolicy(r *DesktopRenameParams) error {
	switch r.OnConflict {
	case "", renameConflictFail, renameConflictKeepBoth, renameConflictUseExisting:
		return nil
	case "replace", "overwrite", "replace_if_identical":
		return fmt.Errorf("on_conflict %q is not supported: a rename never deletes or overwrites the existing file; use fail, keep_both or use_existing_if_identical, or ask the user to remove the existing file", r.OnConflict)
	}
	return fmt.Errorf("on_conflict must be fail, keep_both or use_existing_if_identical")
}

// renameKeepBothName returns the n-th Explorer style alternative of name,
// keeping a displayed extension after the number.
func renameKeepBothName(name string, n int, extensionShown bool) string {
	base, ext := name, ""
	if extensionShown {
		if i := strings.LastIndex(name, "."); i > 0 {
			base, ext = name[:i], name[i:]
		}
	}
	return fmt.Sprintf("%s (%d)%s", base, n, ext)
}

// desktopRenameFullName reports whether full is new extended by one hidden
// extension, or equal to it when extensions are displayed.
func desktopRenameFullName(newName, full string) bool {
	if full == "" {
		return true
	}
	if !desktopRenameName(full) {
		return false
	}
	ext, ok := strings.CutPrefix(full, newName)
	return ok && (ext == "" || strings.HasPrefix(ext, ".") && len(ext) > 1 && !strings.ContainsAny(ext[1:], "."))
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
	if !desktopRenameFullName(p.Rename.NewName, p.Rename.ExpectedFullName) {
		return fantasy.NewTextErrorResponse("invalid_name: expected_full_name must be new_name plus at most one extension (e.g. new_name sonuc, expected_full_name sonuc.txt); no rename input sent"), nil
	}
	if err := validateRenameConflictPolicy(p.Rename); err != nil {
		return errorResponse(computer.NewContractError("invalid_argument", "desktop.rename.on_conflict", err.Error()+"; no rename input sent", "Choose a supported policy and send the rename again.", "")), nil
	}
	rename := *p.Rename
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
	if p.Rename.OldName != rename.NewName {
		q := selector
		q.Name = rename.NewName
		o, result, callErr := desktopFlowFind(ctx, q, child)
		if desktopRecipeStopped(result, callErr) {
			return result, callErr
		}
		if len(o.Elements) != 0 {
			resolved, done, r := resolveRenameConflict(ctx, window, rename, child)
			if done {
				return r, nil
			}
			rename = resolved
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
		if r, err = key("type", "", "", rename.NewName); desktopRecipeStopped(r, err) {
			return r, err
		}
		if _, r, err = readEditor(rename.NewName, editor.ID); desktopRecipeStopped(r, err) {
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
	q := computer.AutomationRequest{WindowID: window.ID, Name: rename.NewName, Role: "ControlType.ListItem", Condition: "text", Expected: rename.NewName, WaitMS: 3000}
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
	// The display label proves the visible name only. Folder, full name and
	// content are separate requirements; a rename can establish the first two
	// from the shell view of this window and never the content.
	requirements := desktopRenameRequirements(ctx, window, &rename, child)
	full := requirements["full_name"].(map[string]any)
	workflow := map[string]any{"mode": "rename", "window_id": window.ID, "old_name": p.Rename.OldName, "actual_name": rename.NewName, "display_name_verified": true, "condition_verified": true, "requirements": requirements, "task_completion_requires_verification": true}
	if rename.NewName != p.Rename.NewName {
		// keep_both chose a free name; the requested one still belongs to the
		// existing item, which was not touched.
		workflow["requested_name"], workflow["conflict"] = p.Rename.NewName, map[string]any{"policy": renameConflictKeepBoth, "existing_item_untouched": true}
	}
	data, _ := json.Marshal(map[string]any{"workflow": workflow})
	if rename.ExpectedFullName != "" && full["verified"] != true {
		// The rename itself happened; only the requested full name is unproven.
		return fantasy.NewTextErrorResponse("full_name_unverified: the item was renamed to the display name " + rename.NewName + ", but its file-system name was not confirmed as " + rename.ExpectedFullName + ". Do not rename again; inspect Explorer properties. " + string(data)), nil
	}
	return fantasy.NewTextResponse(string(data)), nil
}

// desktopRenameRequirements reads the renamed item's file-system name and
// folder from the Explorer view and reports each requirement separately.
func desktopRenameRequirements(ctx context.Context, window desktopWindowInfo, rename *DesktopRenameParams, child desktopFlowChild) map[string]any {
	full := map[string]any{"verified": false, "display_name": rename.NewName, "source": "shell_view_item"}
	folder := map[string]any{"verified": false}
	if rename.ExpectedFullName != "" {
		full["expected"] = rename.ExpectedFullName
	}
	q := computer.AutomationRequest{WindowID: window.ID, Name: rename.NewName, Role: "ControlType.ListItem", Condition: "file_name", Expected: rename.ExpectedFullName}
	r, err := child(ctx, ComputerParams{Action: "assert", Automation: q})
	var read struct {
		Passed    bool    `json:"passed"`
		Actual    *string `json:"actual"`
		Truncated bool    `json:"actual_truncated"`
		WindowID  string  `json:"window_id"`
		Folder    string  `json:"folder_path"`
	}
	if err == nil && !r.IsError && !r.StopTurn && json.Unmarshal([]byte(r.Content), &read) == nil && read.Actual != nil && *read.Actual != "" && !read.Truncated && read.WindowID == window.ID && desktopRenameFullName(rename.NewName, *read.Actual) {
		full["actual"] = *read.Actual
		full["verified"] = rename.ExpectedFullName == "" || read.Passed && *read.Actual == rename.ExpectedFullName
		if read.Folder != "" {
			folder["verified"], folder["path"], folder["source"] = true, read.Folder, "shell_current_folder"
		}
	} else {
		full["reason"] = "the shell view did not resolve exactly one file-system item for this display name"
	}
	return map[string]any{"folder": folder, "full_name": full, "content": map[string]any{"verified": false, "reason": "a rename never reads file content; reopen and check it separately"}}
}

// renameShellFile is one Explorer item's file-system identity and digest.
type renameShellFile struct {
	FullName string `json:"full_name,omitempty"`
	Folder   string `json:"folder,omitempty"`
	Size     int64  `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	Error    string `json:"error,omitempty"`
}

// readRenameShellFile reads the item's file-system name and folder from this
// Explorer window's shell view and hashes the file. It sends no input.
func readRenameShellFile(ctx context.Context, window desktopWindowInfo, name string, child desktopFlowChild) (renameShellFile, fantasy.ToolResponse, error) {
	q := computer.AutomationRequest{WindowID: window.ID, Name: name, Role: "ControlType.ListItem", Condition: "file_name"}
	r, err := child(ctx, ComputerParams{Action: "assert", Automation: q})
	var read struct {
		Actual    *string `json:"actual"`
		Truncated bool    `json:"actual_truncated"`
		WindowID  string  `json:"window_id"`
		Folder    string  `json:"folder_path"`
	}
	if desktopRecipeStopped(r, err) {
		return renameShellFile{}, r, err
	}
	if json.Unmarshal([]byte(r.Content), &read) != nil || read.Actual == nil || *read.Actual == "" || read.Truncated || read.WindowID != window.ID || !desktopRenameFullName(name, *read.Actual) || !filepath.IsAbs(read.Folder) {
		return renameShellFile{Error: "the shell view did not resolve exactly one file-system item for this display name"}, fantasy.ToolResponse{}, nil
	}
	f := renameShellFile{FullName: *read.Actual, Folder: read.Folder}
	r, err = child(ctx, ComputerParams{Action: "file_check", File: &DesktopFileCheck{Path: filepath.Join(read.Folder, *read.Actual), Condition: "digest"}})
	if desktopRecipeStopped(r, err) {
		return f, r, err
	}
	var digest struct {
		Passed bool   `json:"passed"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	}
	if json.Unmarshal([]byte(r.Content), &digest) != nil || !digest.Passed || len(digest.SHA256) != 64 {
		f.Error = "the file read returned no complete digest evidence"
	} else {
		f.Size, f.SHA256 = digest.Size, digest.SHA256
	}
	return f, fantasy.ToolResponse{}, nil
}

// resolveRenameConflict applies the rename's on_conflict policy after an
// item named new_name was found. It either returns the rename to perform
// (keep_both) or a final response; no path deletes or overwrites a file.
func resolveRenameConflict(ctx context.Context, window desktopWindowInfo, rename DesktopRenameParams, child desktopFlowChild) (DesktopRenameParams, bool, fantasy.ToolResponse) {
	source, r, err := readRenameShellFile(ctx, window, rename.OldName, child)
	if desktopRecipeStopped(r, err) {
		return rename, true, renameConflictStop(r, err)
	}
	existing, r, err := readRenameShellFile(ctx, window, rename.NewName, child)
	if desktopRecipeStopped(r, err) {
		return rename, true, renameConflictStop(r, err)
	}
	identical := source.SHA256 != "" && existing.SHA256 != "" && source.Size == existing.Size && source.SHA256 == existing.SHA256
	policy := rename.OnConflict
	if policy == "" {
		policy = renameConflictFail
	}
	evidence := map[string]any{"requested_name": rename.NewName, "policy": policy, "source": source, "existing": existing, "identical": identical, "input_sent": false}
	if err := ctx.Err(); err != nil {
		return rename, true, fantasy.NewTextErrorResponse("canceled: rename conflict was not resolved; no rename input sent")
	}
	conflict := func(reason string) (DesktopRenameParams, bool, fantasy.ToolResponse) {
		data, _ := json.Marshal(evidence)
		return rename, true, errorResponse(computer.NewContractError("name_conflict", "desktop.rename.new_name",
			fmt.Sprintf("an item named %q already exists in this folder; %s. No rename input was sent and no file was changed. Evidence: %s", rename.NewName, reason, data),
			"Pick an outcome explicitly: on_conflict keep_both (rename to the first free \"name (N)\"), on_conflict use_existing_if_identical (only when identical is true), or a different new_name. If the task does not decide this, ask the user which outcome they want; do not delete or overwrite the existing file.", ""))
	}
	switch policy {
	case renameConflictUseExisting:
		if !identical {
			return conflict("use_existing_if_identical requires byte-identical files, and these differ or could not be read")
		}
		if rename.ExpectedFullName != "" && existing.FullName != rename.ExpectedFullName {
			return conflict(fmt.Sprintf("the existing file is named %q, not the expected %q", existing.FullName, rename.ExpectedFullName))
		}
		evidence["outcome"], evidence["source_kept"] = "existing_identical", true
		data, _ := json.Marshal(map[string]any{"workflow": map[string]any{"mode": "rename", "window_id": window.ID, "old_name": rename.OldName, "actual_name": rename.NewName, "conflict": evidence, "task_completion_requires_verification": true}})
		return rename, true, fantasy.NewTextResponse(string(data))
	case renameConflictKeepBoth:
		// A displayed extension stays after the number; a hidden one is
		// appended by the file system and checked through expected_full_name.
		extensionShown := source.FullName != "" && source.FullName == rename.OldName
		hiddenExt := ""
		if rename.ExpectedFullName != "" {
			hiddenExt = strings.TrimPrefix(rename.ExpectedFullName, rename.NewName)
		} else if !extensionShown && source.FullName != "" {
			hiddenExt = strings.TrimPrefix(source.FullName, rename.OldName)
		}
		for n := 2; n <= 20; n++ {
			candidate := renameKeepBothName(rename.NewName, n, extensionShown)
			if !desktopRenameName(candidate) {
				break
			}
			o, r, err := desktopFlowFind(ctx, computer.AutomationRequest{WindowID: window.ID, Name: candidate, Role: "ControlType.ListItem"}, child)
			if desktopRecipeStopped(r, err) {
				if err != nil {
					return rename, true, fantasy.NewTextErrorResponse("canceled: rename conflict was not resolved; no rename input sent")
				}
				return rename, true, r
			}
			if len(o.Elements) != 0 {
				continue
			}
			if source.Folder != "" {
				r, err := child(ctx, ComputerParams{Action: "file_check", File: &DesktopFileCheck{Path: filepath.Join(source.Folder, candidate+hiddenExt), Condition: "absent"}})
				if err == nil && !r.StopTurn && r.IsError && strings.HasPrefix(r.Content, "file_present:") {
					continue
				}
				if desktopRecipeStopped(r, err) {
					return rename, true, renameConflictStop(r, err)
				}
			}
			resolved := rename
			resolved.NewName = candidate
			if rename.ExpectedFullName != "" {
				resolved.ExpectedFullName = candidate + hiddenExt
			}
			return resolved, false, fantasy.ToolResponse{}
		}
		return conflict("keep_both found no free name up to (20)")
	}
	if identical {
		return conflict("the existing file is byte-identical to the source (on_conflict use_existing_if_identical would keep both untouched and report success)")
	}
	return conflict("the files differ or could not be compared")
}

// renameConflictStop preserves a child's pause and metadata. A transport
// failure remains a read failure rather than a fabricated name conflict.
func renameConflictStop(r fantasy.ToolResponse, err error) fantasy.ToolResponse {
	if err != nil {
		r.IsError = true
		r.Content += "\nfile_read_failed: " + err.Error() + "; no rename input sent"
	}
	return r
}
