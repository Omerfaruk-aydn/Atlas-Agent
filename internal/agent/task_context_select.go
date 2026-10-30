package agent

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

const (
	contextMaxSources  = 16
	contextSourceBytes = 4096
	// contextKnowledgeSlots stay free for current project decisions, which are
	// admitted after the source files.
	contextKnowledgeSlots = 2
	contextTestSlots      = 4
	contextRelatedSlots   = 3
	contextMaxTerms       = 24
	contextWindowLeadIn   = 6
)

type contextCandidate struct {
	path, hash, kind, content string
	score                     int
}

// omit names a source that the budget kept out, so the model knows to read it
// itself instead of assuming the packet was complete.
func omit(packet *ContextPacket, path, kind, reason string) {
	if len(packet.Gaps) < 16 {
		packet.Gaps = append(packet.Gaps, "Omitted ("+reason+"): "+kind+" "+path)
	}
}

// omitSummary reports what was left out as one line per kind, so a long list
// of source files cannot push the omitted tests and callers out of the
// bounded gap list.
func omitSummary(packet *ContextPacket, left []contextCandidate, reason string) {
	if len(left) == 0 {
		return
	}
	packet.Truncated = true
	byKind := map[string][]string{}
	var kinds []string
	for _, c := range left {
		if _, ok := byKind[c.kind]; !ok {
			kinds = append(kinds, c.kind)
		}
		byKind[c.kind] = append(byKind[c.kind], c.path)
	}
	for _, kind := range kinds {
		paths := byKind[kind]
		shown := paths[:min(len(paths), 3)]
		line := fmt.Sprintf("Omitted (%s): %d %s file(s), e.g. %s", reason, len(paths), kind, strings.Join(shown, ", "))
		if len(packet.Gaps) < 16 {
			packet.Gaps = append(packet.Gaps, line)
		}
	}
}

// selectContext admits candidates with reserved shares: tests and
// callers/consumers keep their slots even when the owned source alone would
// fill the budget, and unused shares go back to the source files. Whatever is
// left out is listed in the packet's gaps.
func selectContext(already int, owned, tests, related []contextCandidate, packet *ContextPacket) []contextCandidate {
	room := max(0, contextMaxSources-already-contextKnowledgeSlots)
	testQuota := min(len(tests), contextTestSlots, room)
	relatedQuota := min(len(related), contextRelatedSlots, room-testQuota)
	ownedQuota := min(len(owned), room-testQuota-relatedQuota)

	var chosen []contextCandidate
	chosen = append(chosen, owned[:ownedQuota]...)
	chosen = append(chosen, tests[:testQuota]...)
	chosen = append(chosen, related[:relatedQuota]...)

	// Slots a short list did not need go to whatever still waits.
	spare := room - len(chosen)
	var left []contextCandidate
	for _, rest := range [][]contextCandidate{owned[ownedQuota:], tests[testQuota:], related[relatedQuota:]} {
		take := min(len(rest), max(spare, 0))
		chosen = append(chosen, rest[:take]...)
		spare -= take
		left = append(left, rest[take:]...)
	}
	omitSummary(packet, left, "source limit")
	return chosen
}

// taskTerms extracts the identifiers and words a task is about, to find the
// part of a large file it concerns.
func taskTerms(task session.Todo) []string {
	return termsFrom(task.Content, task.ActiveForm, strings.Join(task.AcceptanceCriteria, " "))
}

func termsFrom(text ...string) []string {
	seen := map[string]bool{}
	var terms []string
	for _, chunk := range text {
		for _, word := range strings.FieldsFunc(chunk, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
		}) {
			lower := strings.ToLower(word)
			distinctive := strings.ContainsAny(word, "_") || hasInnerUpper(word) || len(word) >= 7
			if len(word) < 4 || !distinctive || seen[lower] || contextStopword[lower] {
				continue
			}
			seen[lower] = true
			terms = append(terms, lower)
			if len(terms) >= contextMaxTerms {
				return terms
			}
		}
	}
	return terms
}

func hasInnerUpper(word string) bool {
	for i, r := range word {
		if i > 0 && unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

var contextStopword = map[string]bool{
	"should": true, "which": true, "their": true, "there": true, "because": true,
	"function": true, "implement": true, "update": true, "ensure": true, "return": true,
	"returns": true, "following": true, "already": true, "without": true, "existing": true,
}

// termScore counts the distinct task terms a file mentions.
func termScore(content string, terms []string) int {
	lower := strings.ToLower(content)
	score := 0
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score++
		}
	}
	return score
}

// excerptAround returns the whole content when it fits. Otherwise it returns
// a window of whole lines around the best match for the task's terms, falling
// back to the start of the file, with the 1-based lines it covers.
func excerptAround(content string, terms []string, limit int) (text string, start, end int, cut bool) {
	if len(content) <= limit {
		return content, 1, strings.Count(content, "\n") + 1, false
	}
	lines := strings.SplitAfter(content, "\n")
	best, bestHits := 0, 0
	for i, line := range lines {
		lower := strings.ToLower(line)
		hits := 0
		for _, term := range terms {
			if strings.Contains(lower, term) {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = i, hits
		}
	}
	first := 0
	if bestHits > 0 {
		first = max(0, best-contextWindowLeadIn)
	}
	var b strings.Builder
	last := first
	for i := first; i < len(lines); i++ {
		if b.Len()+len(lines[i]) > limit {
			if b.Len() == 0 {
				// One enormous line: keep a valid prefix of it.
				b.WriteString(validPrefix(lines[i], limit))
				last = i
			}
			break
		}
		b.WriteString(lines[i])
		last = i
	}
	return b.String(), first + 1, last + 1, true
}

func validPrefix(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}
