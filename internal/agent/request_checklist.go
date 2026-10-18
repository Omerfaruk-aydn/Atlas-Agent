package agent

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

// A request with several asks loses one of them more often than it gets
// one wrong: the agent finishes the big part and reports done, or a
// compaction paraphrases the request and the small part drops out. When the
// user wrote the request as a list, each item becomes a checklist entry that
// is shown in the system prompt for the whole request (which compaction
// never touches), given to the evaluator to check item by item, and, without
// an evaluator, confirmed once by the agent before the turn ends. After a
// compaction the original wording comes back too, since a summary is
// exactly where nuance is lost.

const (
	checklistMaxItems     = 15
	checklistMaxItemChars = 240
	originalRequestChars  = 4000
)

var (
	listItemPattern   = regexp.MustCompile(`^\s*(?:[-*•+]|\d{1,2}[.)]|[a-zA-Z][.)]|\[[ xX]?\])\s+(.+)$`)
	inlineItemPattern = regexp.MustCompile(`(?:^|\s)(\d{1,2})[.)]\s+`)
)

// requestItems splits a request written as a list into its items. A request
// without at least two list items has no checklist: splitting prose into
// requirements would guess, and a wrong checklist is worse than none.
func requestItems(request string) []string {
	// A message the user added during the work is one more part of the
	// request, whether or not it is written as a list.
	request, additions, _ := strings.Cut(request, requestAdditionMarker)
	items := listItems(request)
	if len(items) < 2 {
		return nil
	}
	for addition := range strings.SplitSeq(additions, requestAdditionMarker) {
		if addition = strings.TrimSpace(addition); addition != "" {
			items = append(items, clipItem(addition))
		}
	}
	return items[:min(len(items), checklistMaxItems)]
}

// listItems returns the list items written in text.
func listItems(text string) []string {
	var items []string
	for line := range strings.SplitSeq(text, "\n") {
		if inline := inlineItems(line); len(inline) > 0 {
			items = append(items, inline...)
			continue
		}
		if m := listItemPattern.FindStringSubmatch(line); m != nil {
			items = append(items, clipItem(m[1]))
		}
	}
	return items
}

// inlineItems splits "1. do this 2. do that" written on one line, when the
// numbers run in sequence from 1.
func inlineItems(line string) []string {
	marks := inlineItemPattern.FindAllStringSubmatchIndex(line, -1)
	if len(marks) < 2 {
		return nil
	}
	var items []string
	for i, m := range marks {
		if line[m[2]:m[3]] != fmt.Sprint(i+1) {
			return nil
		}
		end := len(line)
		if i+1 < len(marks) {
			end = marks[i+1][0]
		}
		if item := strings.TrimSpace(line[m[1]:end]); item != "" {
			items = append(items, clipItem(item))
		}
	}
	return items
}

func clipItem(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= checklistMaxItemChars {
		return s
	}
	cut := checklistMaxItemChars
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// requestChecklistBlock is the system prompt part for the current request:
// its checklist, and its original wording once the session was compacted.
func (a *sessionAgent) requestChecklistBlock(sessionID string) string {
	if a.isSubAgent || !a.harnessOptions().RequestChecklistEnabled() {
		return ""
	}
	ledger, ok := a.evaluatorLedgers.Get(sessionID)
	if !ok {
		return ""
	}
	ledger.mu.Lock()
	request, compacted := ledger.request, ledger.compacted
	ledger.mu.Unlock()
	var b strings.Builder
	if items := requestItems(request); len(items) > 0 {
		b.WriteString("\n\n<request_checklist>\nThe current request has these parts. Each must be done, or reported as not done with the reason, before you finish:\n")
		for i, item := range items {
			fmt.Fprintf(&b, "%d. %s\n", i+1, item)
		}
		b.WriteString("</request_checklist>")
	}
	if compacted && strings.TrimSpace(request) != "" {
		text := strings.TrimSpace(request)
		if len(text) > originalRequestChars {
			cut := originalRequestChars
			for cut > 0 && !utf8.RuneStart(text[cut]) {
				cut--
			}
			text = text[:cut] + "… (truncated)"
		}
		b.WriteString("\n\n<original_request>\nThe conversation was summarized during this request. The user's request, word for word:\n" + text + "\n</original_request>")
	}
	return b.String()
}

// markCompacted records that the session's request outlived a summary.
func (a *sessionAgent) markCompacted(sessionID string) {
	ledger, ok := a.evaluatorLedgers.Get(sessionID)
	if !ok {
		return
	}
	ledger.mu.Lock()
	ledger.compacted = true
	ledger.mu.Unlock()
}

// confirmChecklist asks the agent, once per request, to account for each
// part of a multi-part request when no evaluator checks it. The caller
// holds ledger.mu.
func (a *sessionAgent) confirmChecklist(sessionID string, ledger *evaluatorLedger) (evaluatorDecision, bool) {
	if ledger.checklistAsked || !a.harnessOptions().RequestChecklistEnabled() {
		return evaluatorDecision{}, false
	}
	items := requestItems(ledger.request)
	if len(items) == 0 {
		return evaluatorDecision{}, false
	}
	ledger.checklistAsked = true
	a.recordHarness(harnessmetrics.ChecklistAsked, sessionID, map[string]string{"items": fmt.Sprint(len(items))})
	var b strings.Builder
	b.WriteString("Before you finish, check the work against each part of the request:\n")
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nFor any part that is not done yet, do it now. Then give the final report with one line per part: done, with the file or command that shows it, or not done and why.")
	return evaluatorDecision{Continue: b.String()}, true
}

var unmetRequirementPattern = regexp.MustCompile(`(?i)^\s*-\s*\[(missing|partial)\]\s*(.+)$`)

// unmetRequirements turns the checklist lines a reviewer marked missing or
// partial into findings.
func unmetRequirements(lines []string) string {
	var out []string
	for _, line := range lines {
		if m := unmetRequirementPattern.FindStringSubmatch(line); m != nil {
			out = append(out, "- Requirement "+strings.ToLower(m[1])+": "+strings.TrimSpace(m[2]))
		}
	}
	return strings.Join(out, "\n")
}
