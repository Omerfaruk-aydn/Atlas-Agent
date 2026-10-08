package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// browserVerifiedDelivery counts only a committed, valid field change.
// Clicks, drags and keystrokes can merely select or navigate; their dispatch
// is not delivery evidence. Canvas work advances when task_update verifies
// a requirement. A partial form can still contain a proven field change.
func browserVerifiedDelivery(action string, resp fantasy.ToolResponse) bool {
	if action != "set_field" && action != "select_option" && action != "fill_form" {
		return false
	}
	data := strings.TrimSpace(resp.Content)
	if !json.Valid([]byte(data)) {
		if last := strings.LastIndex(data, "\n"); last >= 0 {
			data = data[last+1:]
		}
	}
	changed := func(field *browser.FieldResult) bool {
		return field != nil && field.Succeeded() && field.Verification == "verified" &&
			(field.Valid == nil || *field.Valid) &&
			(field.Accepted != field.Before || field.Acceptance == "shown_mode")
	}
	if action == "fill_form" {
		var form browser.FormResult
		if json.Unmarshal([]byte(data), &form) != nil {
			return false
		}
		for _, entry := range form.Fields {
			if !entry.Skipped && changed(entry.Field) {
				return true
			}
		}
		return false
	}
	var field browser.FieldResult
	return json.Unmarshal([]byte(data), &field) == nil && changed(&field)
}

// fieldEvidenceKey identifies a resolved property independently of the
// selector, ref or label used to reach it. Values never enter the key.
func fieldEvidenceKey(field browser.FieldResult) string {
	if field.Name == "" || field.DocumentID == "" {
		return ""
	}
	data, _ := json.Marshal([]string{field.FrameID, field.DocumentID, field.Context, field.Name})
	sum := sha256.Sum256(data)
	return "field:" + hex.EncodeToString(sum[:16])
}

func legacyEvidenceKey(target string) string {
	if target == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(target))
	return "target:" + hex.EncodeToString(sum[:16])
}

// resolvedEvidenceChanged can match a field written before a batch stopped
// on a tab change. Document-bound keys remain identifiable even when the
// op's final active tab differs; display labels alone cannot cross tabs.
func resolvedEvidenceChanged(changed, earlier []string) bool {
	for _, key := range changed {
		if strings.HasPrefix(key, "field:") && slices.Contains(earlier, key) {
			return true
		}
	}
	return false
}

// fieldOperationEvidence preserves per-field effects, including successful
// or uncertain writes in a batch whose overall response is an error.
func fieldOperationEvidence(action string, params BrowserParams, resp fantasy.ToolResponse) (targets, changed []string, uncertain bool) {
	if action != "set_field" && action != "select_option" && action != "read_field" && action != "fill_form" {
		return nil, nil, false
	}
	data := strings.TrimSpace(resp.Content)
	if !json.Valid([]byte(data)) {
		// browserJSONError places compact JSON after the final newline.
		// A brace in the error's quoted field value is not the JSON start.
		if last := strings.LastIndex(data, "\n"); last >= 0 {
			data = data[last+1:]
		}
	}
	if !json.Valid([]byte(data)) {
		code, _ := browserErrorCode(resp.Content)
		dispatch := dispatchState(action, resp, code)
		return nil, nil, action != "read_field" && (dispatch == "sent" || dispatch == "unknown")
	}
	add := func(field browser.FieldResult, fallback string, skipped bool) {
		key := fieldEvidenceKey(field)
		if key == "" {
			key = legacyEvidenceKey(fallback)
		}
		if key != "" && !slices.Contains(targets, key) {
			targets = append(targets, key)
		}
		if skipped || action == "read_field" || (field.Dispatch != "sent" && field.Dispatch != "unknown") {
			return
		}
		if key == "" {
			uncertain = true
		} else if !slices.Contains(changed, key) {
			changed = append(changed, key)
		}
	}
	if action == "fill_form" {
		var form browser.FormResult
		if json.Unmarshal([]byte(data), &form) != nil {
			return nil, nil, true
		}
		for _, entry := range form.Fields {
			if entry.Field == nil {
				continue
			}
			fallback := ""
			if entry.Index >= 0 && entry.Index < len(params.Advanced.Fields) {
				f := params.Advanced.Fields[entry.Index]
				fallback = fieldTargetDescription(f.Ref, f.Selector, f.Role, f.Name, f.Label)
			}
			add(*entry.Field, fallback, entry.Skipped)
		}
		return targets, changed, uncertain
	}
	var field browser.FieldResult
	if json.Unmarshal([]byte(data), &field) != nil {
		return nil, nil, false
	}
	add(field, fieldTargetDescription(params.Ref, firstNonEmpty(params.Selector, params.Advanced.Selector), params.Advanced.Role, params.Advanced.Name, params.Advanced.Label), false)
	return targets, changed, uncertain
}

func fieldTargetDescription(ref, selector, role, name, label string) string {
	if ref != "" {
		return "ref " + ref
	}
	if selector != "" {
		return selector
	}
	return strings.TrimSpace(role + " " + name + " " + label)
}

// browserOpInvalidates is shared by checkpoint invalidation and attempts
// to attach old evidence again. A partially failed batch follows the same
// rules as direct field writes.
func browserOpInvalidates(later BrowserOp, action, target string, keys []string) bool {
	if later.ChangeUncertain {
		return true
	}
	// A whole-page observation or image has no narrower target. Any field
	// write makes that earlier description of the page require a new check.
	if len(later.ChangedKeys) > 0 && target == "" && len(keys) == 0 {
		return true
	}
	if len(keys) == 0 {
		if key := legacyEvidenceKey(target); key != "" {
			keys = []string{key}
		}
	}
	for _, key := range later.ChangedKeys {
		if slices.Contains(keys, key) {
			return true
		}
	}
	if !later.OK || !browserMutating(later.Action) || later.Dispatch != "sent" || later.Verification == "failed" {
		return false
	}
	if later.Target != "" && target == later.Target {
		return true
	}
	return later.Action == "undo" && (action == "set_field" || action == "select_option" || action == "fill_form" || action == "read_field")
}
