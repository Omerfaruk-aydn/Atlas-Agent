package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
)

// Observation output limits. Full listings keep the previous cap; change
// lists are smaller because they only carry what moved. Attention lines
// (dialogs, alerts, invalid fields, focus) are never cut by either limit.
const (
	fullObservationLimit  = 150
	deltaObservationLimit = 80
	removedRefLimit       = 40
)

// observationBase is the last observation shown to the model for one
// browser session. A change-focused result is only computed against a base
// from the same driver, tab and document; anything else is a full result.
type observationBase struct {
	owner    browser.Session
	tab      string
	doc      string
	id       string
	elements map[string]browser.SnapshotElement
	order    []string
}

type observationTracker struct {
	mu     sync.Mutex
	seq    map[string]int
	bases  map[string]*observationBase
	grants map[string]*observationGrant
}

// observationGrant records reference provenance independently of the full
// page delta base. Scoped observations authorize their refs without erasing
// the base used to compare the rest of the page.
type observationGrant struct {
	owner browser.Session
	exec  browser.ExecutionIdentity
	id    string
	// refs maps each authorized ref to the identity the model was shown,
	// or to a zero identity when only the ref itself is known.
	refs map[string]browser.RefIdentity
}

var browserObservations = &observationTracker{seq: map[string]int{}, bases: map[string]*observationBase{}, grants: map[string]*observationGrant{}}

// grantScreenshotRefs binds only refs actually labeled in this capture.
// A navigation or reconnect between capture and return issues no grants.
func grantScreenshotRefs(ctx context.Context, sessionID string, sess browser.Session) {
	if capture, ok := sess.(browser.ScreenshotRefSession); ok {
		exec, refs := capture.ScreenshotReferences()
		if exec.Document != "" && exec == referenceIdentity(ctx, sess) {
			browserObservations.grantRefs(sessionID, sess, exec, "", refs)
		}
	}
}

// invalidate drops the base for a session, for example after close.
func (t *observationTracker) invalidate(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.bases, sessionID)
	delete(t.grants, sessionID)
}

func (t *observationTracker) current(sessionID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if b := t.grants[sessionID]; b != nil {
		return b.id
	}
	return ""
}

func referenceIdentity(ctx context.Context, sess browser.Session) browser.ExecutionIdentity {
	if driver, ok := sess.(browser.IdentitySession); ok {
		return driver.ExecutionIdentity(ctx)
	}
	if tabs, ok := sess.(browser.TabIdentity); ok {
		return browser.ExecutionIdentity{TabID: tabs.ActiveTab()}
	}
	return browser.ExecutionIdentity{}
}

// grantRefs merges observations only while the driver, tab and document
// match. A reconnect or navigation never authorizes earlier refs.
func (t *observationTracker) grantRefs(sessionID string, sess browser.Session, exec browser.ExecutionIdentity, id string, refs map[string]browser.RefIdentity) {
	t.mu.Lock()
	defer t.mu.Unlock()
	g := t.grants[sessionID]
	if g == nil || g.owner != sess || g.exec != exec {
		g = &observationGrant{owner: sess, exec: exec, refs: map[string]browser.RefIdentity{}}
		t.grants[sessionID] = g
	}
	if id != "" {
		g.id = id
	}
	for ref, identity := range refs {
		if ref != "" {
			g.refs[ref] = identity
		}
	}
}

// grantedRef returns the identity the model was shown for ref, and whether
// ref is authorized in this browser, tab and document at all.
func (t *observationTracker) grantedRef(sessionID string, sess browser.Session, exec browser.ExecutionIdentity, ref string) (browser.RefIdentity, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	g := t.grants[sessionID]
	if g == nil || g.owner != sess || g.exec != exec {
		return browser.RefIdentity{}, false
	}
	identity, ok := g.refs[ref]
	return identity, ok
}

// grantFoundRefs authorizes refs a find returned, recording their identity
// as observation names it when the driver can report it.
func (t *observationTracker) grantFoundRefs(ctx context.Context, sessionID string, sess browser.Session, exec browser.ExecutionIdentity, refs []string) {
	granted := map[string]browser.RefIdentity{}
	for _, ref := range refs {
		granted[ref] = browser.RefIdentity{}
	}
	if identities, ok := sess.(browser.RefIdentitySession); ok && len(refs) > 0 {
		if current, err := identities.RefIdentities(ctx, refs); err == nil {
			for ref, identity := range current {
				granted[ref] = identity
			}
		}
	}
	t.grantRefs(sessionID, sess, exec, "", granted)
}

// observeMode is full, delta or scope. Delta silently becomes full when no
// valid base exists, and says so.
type observeRequest struct {
	mode   string
	scope  string
	full   bool
	cursor int
	limit  int
}

type observation struct {
	id       string
	baseID   string
	mode     string
	reason   string
	tab      string
	doc      string
	url      string
	title    string
	elements []browser.SnapshotElement
	dialogs  []browser.DialogInfo
	text     string
}

// takeObservation observes the page and renders it against the session's
// base, then makes it the new base.
func takeObservation(ctx context.Context, sess browser.Session, sessionID string, req observeRequest) (observation, error) {
	before := referenceIdentity(ctx, sess)
	var obs browser.PageObservation
	if observer, ok := sess.(browser.ObservingSession); ok {
		var err error
		obs, err = observer.Observe(ctx, browser.ObserveRequest{Full: req.full, Scope: req.scope})
		if err != nil {
			return observation{}, err
		}
	} else {
		if req.scope != "" {
			return observation{}, fmt.Errorf("scoped observation is unavailable in this browser driver")
		}
		elements, err := sess.Snapshot(req.full)
		if err != nil {
			return observation{}, err
		}
		url, _ := sess.URL()
		obs = browser.PageObservation{Document: url, URL: url, Elements: elements}
	}
	after := referenceIdentity(ctx, sess)
	if before != after {
		return observation{}, fmt.Errorf("stale_document: browser context changed during observation; observe the intended document again")
	}
	if _, strict := sess.(browser.IdentitySession); strict && after.Document == "" {
		return observation{}, fmt.Errorf("stale_document: the current document identity is unavailable; no refs were authorized")
	}
	t := browserObservations
	t.mu.Lock()
	t.seq[sessionID]++
	id := fmt.Sprintf("o%d", t.seq[sessionID])
	base := t.bases[sessionID]
	out := observation{id: id, mode: req.mode, tab: obs.TabID, doc: obs.Document, url: obs.URL, title: obs.Title, elements: obs.Elements, dialogs: sess.PendingDialogs()}
	switch {
	case req.mode != "delta":
	case base == nil:
		out.mode, out.reason = "full", "no earlier observation in this session"
	case base.owner != sess:
		out.mode, out.reason = "full", "browser session was restarted or reconnected"
	case base.tab != obs.TabID:
		out.mode, out.reason = "full", "a different tab is selected"
	case base.doc != obs.Document:
		out.mode, out.reason = "full", "the page navigated to a new document"
	default:
		out.baseID = base.id
	}
	// A scoped observation never replaces the whole-page base: its absent
	// elements were only outside the scope.
	if req.mode != "scope" {
		next := &observationBase{owner: sess, tab: obs.TabID, doc: obs.Document, id: id, elements: map[string]browser.SnapshotElement{}}
		for _, el := range obs.Elements {
			next.elements[el.Ref] = el
			next.order = append(next.order, el.Ref)
		}
		if out.mode == "delta" && base != nil && !req.full {
			// Elements outside a viewport-only observation are not gone;
			// keep them in the base so scrolling back is not "added".
			for _, ref := range base.order {
				if _, ok := next.elements[ref]; !ok {
					if el := base.elements[ref]; !el.Visible {
						next.elements[ref] = el
						next.order = append(next.order, ref)
					}
				}
			}
		}
		t.bases[sessionID] = next
	}
	t.mu.Unlock()
	refs := make(map[string]browser.RefIdentity, len(obs.Elements))
	for _, el := range obs.Elements {
		refs[el.Ref] = browser.RefIdentity{Role: el.Role, Name: el.Name}
	}
	t.grantRefs(sessionID, sess, after, id, refs)
	limit := req.limit
	if out.mode == "delta" {
		if limit <= 0 {
			limit = deltaObservationLimit
		}
		out.text = renderDelta(out, base, limit)
	} else {
		if limit <= 0 {
			limit = fullObservationLimit
		}
		out.text = renderFull(out, req.cursor, limit)
	}
	return out, nil
}

// elementDigest is what counts as a change: role, name, value and state.
// Geometry is excluded, so layout shifts do not flood the change list.
func elementDigest(el browser.SnapshotElement) string {
	return strings.Join([]string{
		el.Role, el.Name, el.Value, el.Checked, el.Expanded, el.Error, el.Context, el.Hint,
		fmt.Sprint(el.Selected, el.Disabled, el.Focused, el.Invalid),
	}, "\x00")
}

func attention(el browser.SnapshotElement) bool {
	switch el.Role {
	case "dialog", "alertdialog", "alert":
		return true
	}
	return el.Invalid || el.Focused
}

// formatElement renders one element line. Context is printed per group by
// the caller; flags carry the state verification needs.
func formatElement(el browser.SnapshotElement) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s", el.Ref, el.Role)
	if el.Name != "" {
		fmt.Fprintf(&b, " %q", el.Name)
	} else if el.Hint != "" {
		fmt.Fprintf(&b, " (label? %q)", el.Hint)
	}
	if el.Value != "" {
		fmt.Fprintf(&b, " value=%q", el.Value)
	}
	var flags []string
	if el.Focused {
		flags = append(flags, "focused")
	}
	if el.Checked != "" {
		flags = append(flags, "checked="+el.Checked)
	}
	if el.Selected {
		flags = append(flags, "selected")
	}
	if el.Expanded != "" {
		flags = append(flags, "expanded="+el.Expanded)
	}
	if el.Disabled {
		flags = append(flags, "disabled")
	}
	if el.Invalid {
		flags = append(flags, "invalid")
	}
	if el.Shadow {
		flags = append(flags, "shadow")
	}
	if len(flags) > 0 {
		fmt.Fprintf(&b, " {%s}", strings.Join(flags, ","))
	}
	if el.Error != "" {
		fmt.Fprintf(&b, " error=%q", el.Error)
	}
	if el.Tag != "" && el.Tag != el.Role {
		fmt.Fprintf(&b, " (%s)", el.Tag)
	}
	return b.String()
}

func observationHeader(o observation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "observation_id=%s", o.id)
	if o.baseID != "" {
		fmt.Fprintf(&b, " base_observation_id=%s", o.baseID)
	}
	fmt.Fprintf(&b, " mode=%s", o.mode)
	if o.reason != "" {
		fmt.Fprintf(&b, " (full because %s)", o.reason)
	}
	if o.tab != "" {
		fmt.Fprintf(&b, " tab=%s", o.tab)
	}
	if o.url != "" {
		fmt.Fprintf(&b, "\nurl=%s", o.url)
	}
	if o.title != "" {
		fmt.Fprintf(&b, " title=%q", o.title)
	}
	b.WriteString("\n")
	if len(o.dialogs) > 0 {
		fmt.Fprintf(&b, "%d pending dialog(s) block the page -- resolve with dialog_wait/dialog_handle first:\n", len(o.dialogs))
		for _, d := range o.dialogs {
			fmt.Fprintf(&b, "- %s: %q\n", d.Type, d.Message)
		}
	}
	return b.String()
}

// writeGrouped prints elements grouped under their container context, so a
// panel's name appears once instead of on every line.
func writeGrouped(b *strings.Builder, elements []browser.SnapshotElement, prefix string) {
	context := "\x00"
	for _, el := range elements {
		if el.Context != context {
			context = el.Context
			if context == "" {
				b.WriteString("(page)\n")
			} else {
				fmt.Fprintf(b, "%s:\n", context)
			}
		}
		b.WriteString(prefix)
		b.WriteString(formatElement(el))
		b.WriteString("\n")
	}
}

func renderFull(o observation, cursor, limit int) string {
	var b strings.Builder
	b.WriteString(observationHeader(o))
	var urgent, rest []browser.SnapshotElement
	for _, el := range o.elements {
		if attention(el) {
			urgent = append(urgent, el)
		} else {
			rest = append(rest, el)
		}
	}
	if len(urgent) > 0 && cursor == 0 {
		b.WriteString("attention:\n")
		for _, el := range urgent {
			b.WriteString("  " + formatElement(el))
			if el.Context != "" {
				fmt.Fprintf(&b, " in %s", el.Context)
			}
			b.WriteString("\n")
		}
	}
	if len(o.elements) == 0 {
		b.WriteString("No interactive elements found.\n")
		return b.String()
	}
	if cursor < 0 || cursor > len(rest) {
		cursor = 0
	}
	end := min(len(rest), cursor+limit)
	writeGrouped(&b, rest[cursor:end], "")
	if end < len(rest) {
		fmt.Fprintf(&b, "truncated: has_more=true shown=%d-%d of %d. Continue with observe advanced.cursor=%d, or narrow with mode=scope.\n", cursor+1, end, len(rest), end)
	}
	return b.String()
}

func renderDelta(o observation, base *observationBase, limit int) string {
	var b strings.Builder
	b.WriteString(observationHeader(o))
	var added, changed []browser.SnapshotElement
	var before []browser.SnapshotElement
	present := map[string]bool{}
	for _, el := range o.elements {
		present[el.Ref] = true
		old, ok := base.elements[el.Ref]
		switch {
		case !ok:
			added = append(added, el)
		case elementDigest(old) != elementDigest(el):
			changed = append(changed, el)
			before = append(before, old)
		}
	}
	var removed []string
	for _, ref := range base.order {
		if !present[ref] && base.elements[ref].Visible {
			removed = append(removed, ref)
		}
	}
	if len(added)+len(changed)+len(removed) == 0 {
		fmt.Fprintf(&b, "No changes since %s (%d elements unchanged). A dispatched action without a visible change may not have taken effect; verify with a specific assert or read_field.\n", base.id, len(o.elements))
		return b.String()
	}
	shown := 0
	// Changes first: they are usually the effect being verified.
	if len(changed) > 0 {
		fmt.Fprintf(&b, "changed (%d):\n", len(changed))
		for i, el := range changed {
			if shown >= limit && !attention(el) {
				continue
			}
			shown++
			b.WriteString("  " + formatElement(el))
			if diff := describeChange(before[i], el); diff != "" {
				b.WriteString("  <- was " + diff)
			}
			if el.Context != "" {
				fmt.Fprintf(&b, " in %s", el.Context)
			}
			b.WriteString("\n")
		}
	}
	if len(added) > 0 {
		fmt.Fprintf(&b, "added (%d):\n", len(added))
		var keep []browser.SnapshotElement
		for _, el := range added {
			if shown >= limit && !attention(el) {
				continue
			}
			shown++
			keep = append(keep, el)
		}
		writeGrouped(&b, keep, "  ")
	}
	if len(removed) > 0 {
		list := removed
		if len(list) > removedRefLimit {
			list = list[:removedRefLimit]
		}
		fmt.Fprintf(&b, "removed (%d): %s", len(removed), strings.Join(list, " "))
		if len(removed) > len(list) {
			fmt.Fprintf(&b, " ... (+%d)", len(removed)-len(list))
		}
		b.WriteString("\n")
	}
	total := len(added) + len(changed)
	if shown < total {
		fmt.Fprintf(&b, "truncated: has_more=true showed %d of %d changed/added elements. Observe with mode=full or mode=scope for the rest.\n", shown, total)
	}
	fmt.Fprintf(&b, "unchanged: %d elements omitted (refs from %s remain valid).\n", len(o.elements)-total, base.id)
	return b.String()
}

func describeChange(old, el browser.SnapshotElement) string {
	var parts []string
	if old.Role != el.Role {
		parts = append(parts, "role="+old.Role)
	}
	if old.Name != el.Name {
		parts = append(parts, fmt.Sprintf("name=%q", old.Name))
	}
	if old.Value != el.Value {
		parts = append(parts, fmt.Sprintf("value=%q", old.Value))
	}
	if old.Checked != el.Checked {
		parts = append(parts, "checked="+old.Checked)
	}
	if old.Expanded != el.Expanded {
		parts = append(parts, "expanded="+old.Expanded)
	}
	if old.Focused != el.Focused {
		parts = append(parts, fmt.Sprintf("focused=%v", old.Focused))
	}
	if old.Disabled != el.Disabled {
		parts = append(parts, fmt.Sprintf("disabled=%v", old.Disabled))
	}
	if old.Invalid != el.Invalid || old.Error != el.Error {
		parts = append(parts, fmt.Sprintf("invalid=%v", old.Invalid))
	}
	if old.Context != el.Context {
		parts = append(parts, fmt.Sprintf("context=%q", old.Context))
	}
	if old.Role != el.Role || old.Hint != el.Hint && old.Hint != "" && el.Hint != "" {
		parts = append(parts, "(same ref now describes a different control; the page reused the element, so resolve it again by label)")
	}
	return strings.Join(parts, " ")
}
