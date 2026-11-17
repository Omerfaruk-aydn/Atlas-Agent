package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// minCheckpointChars is the shortest compaction summary worth keeping.
	minCheckpointChars = 200
	// maxPendingProposals stops automatic proposals from piling up unread.
	maxPendingProposals  = 10
	maxCheckpointSources = 8
)

var (
	pathLikeRe = regexp.MustCompile("[A-Za-z0-9_][A-Za-z0-9_./\\\\-]*\\.[A-Za-z0-9]{1,6}")
	secretLike = regexp.MustCompile(`(?i)(sk-[a-z0-9_-]{20,}|ghp_[a-z0-9]{30,}|github_pat_[a-z0-9_]{30,}|AKIA[0-9A-Z]{16}|xox[baprs]-[a-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|(api[_-]?key|secret|token|password)["']?\s*[:=]\s*["']?[a-z0-9_\-./+=]{16,})`)
)

// ProposeCheckpoint turns a compaction summary into a proposed item, so what
// a long session worked out is not lost with its history. It records the
// project files the summary names as sources, which makes the note go stale
// when they change. It proposes nothing, and says why, when the summary is
// short, contains something that looks like a credential, or too many
// proposals are already waiting for review. Nothing it writes is used until
// the user approves it.
func (s *Store) ProposeCheckpoint(summary string, now time.Time) (Item, string, error) {
	summary = strings.TrimSpace(summary)
	if len(summary) < minCheckpointChars {
		return Item{}, "the summary is too short to keep", nil
	}
	if secretLike.MatchString(summary) {
		return Item{}, "the summary looks like it contains a credential", nil
	}
	items, err := s.List()
	if err != nil {
		return Item{}, "", err
	}
	pending := 0
	for _, it := range items {
		if it.Status == Proposed {
			pending++
		}
	}
	if pending >= maxPendingProposals {
		return Item{}, fmt.Sprintf("%d proposals are already waiting for review", pending), nil
	}
	title := "Checkpoint " + now.UTC().Format("2006-01-02 15:04")
	line := ""
	for _, l := range strings.Split(summary, "\n") {
		if l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#>-* ")); l != "" {
			line = l
			break
		}
	}
	if len(line) > maxSummary {
		line = strings.TrimSpace(line[:maxSummary-3]) + "..."
	}
	it, err := s.Propose(title, line, summary, s.namedSources(summary))
	if err != nil {
		return Item{}, "", err
	}
	return it, "", nil
}

// namedSources lists the project files a text mentions, up to a limit.
func (s *Store) namedSources(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range pathLikeRe.FindAllString(text, -1) {
		rel := filepath.ToSlash(strings.Trim(m, "./\\"))
		if rel == "" || seen[rel] || strings.Contains(rel, "..") || filepath.IsAbs(m) {
			continue
		}
		seen[rel] = true
		if info, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(rel))); err == nil && info.Mode().IsRegular() {
			out = append(out, rel)
			if len(out) == maxCheckpointSources {
				break
			}
		}
	}
	return out
}
