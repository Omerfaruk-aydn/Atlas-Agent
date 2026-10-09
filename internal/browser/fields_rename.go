package browser

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// RenamedFieldName returns the accessible name a field is expected to carry
// after its value changed from before to after, when the application puts
// the value into the name ("Font size: 12 px" becomes "Font size: 14 px").
// It returns "" when the name does not contain the old value exactly once,
// so a name that cannot be rewritten unambiguously is never guessed.
func RenamedFieldName(name, before, after string) string {
	before = strings.TrimSpace(before)
	if name == "" || before == "" || before == after || strings.Count(name, before) != 1 {
		return ""
	}
	return strings.Replace(name, before, strings.TrimSpace(after), 1)
}

// SameSelection reports whether two selection fingerprints describe the
// same selected objects. An entry carries a per-node counter followed by
// the application's own ids; when the application supplies ids the counter
// is ignored, because a re-rendered row is a new node for the same object.
// Without application ids the node itself must be the same.
func SameSelection(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	norm := func(list []string) []string {
		out := make([]string, len(list))
		for i, entry := range list {
			out[i] = selectionKey(entry)
		}
		slices.Sort(out)
		return out
	}
	return slices.Equal(norm(a), norm(b))
}

func selectionKey(entry string) string {
	end := strings.Index(entry, "]")
	if !strings.HasPrefix(entry, "[") || end < 0 {
		return entry
	}
	var ids []string
	if json.Unmarshal([]byte(entry[:end+1]), &ids) != nil || len(ids) == 0 {
		return entry
	}
	appIDs := ids[1:]
	if slices.ContainsFunc(appIDs, func(id string) bool { return id != "" }) {
		return strings.Join(appIDs, "\x00") + entry[end+1:]
	}
	return entry
}

// renamedFieldPolls and renamedFieldPollEvery bound the wait for a rebuilt
// field to appear: about one and a half seconds.
const (
	renamedFieldPolls     = 10
	renamedFieldPollEvery = 150 * time.Millisecond
)

// resolveRenamedField reads back a field whose node or name changed after
// the commit. It resolves the same role with the name rewritten for the
// requested value (or the same name, when the name does not carry the
// value) and accepts the result only when exactly one field
// matches in the same frame and document and the page's selection is the
// selection the field edited before input. Nothing is typed.
func (s *chromedpSession) resolveRenamedField(ctx context.Context, frame fieldFrame, prep fieldScriptResult, target FieldTarget, expected string, window time.Duration) (fieldScriptResult, fieldSettle, bool) {
	name := RenamedFieldName(prep.Name, prep.Before, expected)
	if name == "" {
		// A field whose name does not carry its value can still be
		// remounted (Figma rebuilds Height when it turns from Hug to
		// Fixed); it is looked up under the same name.
		name = prep.Name
	}
	if name == "" || prep.IdentityDetails == nil {
		return fieldScriptResult{}, fieldSettle{}, false
	}
	if err := s.verifyFieldDocument(ctx, frame); err != nil {
		return fieldScriptResult{}, fieldSettle{}, false
	}
	renamed := FieldTarget{Role: prep.Role, Name: name, Scope: target.Scope, Within: target.Within, WithinRole: target.WithinRole}
	// A replacement gets its own full settle window. Stability observed on
	// the vanished node says nothing about this new field.
	started := time.Now()
	deadline := started.Add(time.Duration(renamedFieldPolls)*renamedFieldPollEvery + window + max(window, 2*time.Second))
	var appeared, lastChange time.Time
	lastKey := ""
	var r fieldScriptResult
	for {
		var err error
		r, err = s.fieldScript(ctx, frame, map[string]any{"op": "resolve", "target": renamed})
		if err != nil || s.verifyFieldDocument(ctx, frame) != nil {
			return fieldScriptResult{}, fieldSettle{}, false
		}
		if r.OK && r.Count == 1 && r.IdentityDetails != nil {
			if !SameSelection(prep.IdentityDetails.Selection, r.IdentityDetails.Selection) {
				return fieldScriptResult{}, fieldSettle{}, false
			}
			now := time.Now()
			if appeared.IsZero() {
				appeared, lastChange = now, now
			}
			data, _ := json.Marshal([]any{r.Before, r.OptionValue, r.Name, r.Valid, r.Message, r.Busy})
			key := string(data)
			if key != lastKey {
				lastKey, lastChange = key, now
			}
			if now.Sub(appeared) >= window && now.Sub(lastChange) >= min(window, fieldQuiet) && !r.Busy {
				return r, fieldSettle{observed: now.Sub(started), stable: now.Sub(lastChange)}, true
			}
		} else {
			appeared, lastChange, lastKey = time.Time{}, time.Time{}, ""
		}
		if time.Now().After(deadline) {
			return fieldScriptResult{}, fieldSettle{}, false
		}
		select {
		case <-ctx.Done():
			return fieldScriptResult{}, fieldSettle{}, false
		case <-time.After(renamedFieldPollEvery):
		}
	}
}

// inlineRenameLanded reports that an inline rename editor (a page, layer or
// file name field) closed on commit and left the requested name in the
// edited object's original parent and child slot. Document-wide matching
// text cannot establish a rename.
func (s *chromedpSession) inlineRenameLanded(ctx context.Context, frame fieldFrame, token string, prep fieldScriptResult, expected string) bool {
	if prep.NamedBefore == nil || strings.TrimSpace(expected) == "" || prep.Kind != "text" {
		return false
	}
	if err := s.verifyFieldDocument(ctx, frame); err != nil {
		return false
	}
	r, err := s.fieldScript(ctx, frame, map[string]any{"op": "named", "token": token, "value": expected})
	if err != nil || !r.OK || r.Connected {
		return false
	}
	return r.Count > *prep.NamedBefore
}
