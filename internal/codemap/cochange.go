package codemap

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Files that change in the same commits belong together even when no
// import says so: a component and its test, a handler and its route, a
// schema and its migration. The history is read once per commit at HEAD.

const (
	// historyCommits is how far back the history is read.
	historyCommits = 600
	// maxCommitFiles skips sweeping commits (a rename, a reformat) whose
	// files say nothing about each other.
	maxCommitFiles = 40
	// minTogether is the least number of shared commits that counts.
	minTogether = 2
)

// Related is a file that changed together with another.
type Related struct {
	Path string
	// Together is the number of commits that changed both files, and
	// Commits the number that changed the first one.
	Together, Commits int
}

type cochange struct {
	head    string
	commits map[string]int
	pairs   map[string]map[string]int
}

// Related returns the files that most often changed together with file,
// strongest first, or nil without git history.
func (m *Map) Related(ctx context.Context, file string, limit int) []Related {
	if m.history == nil {
		return nil
	}
	h := m.history.historyFor(ctx, m.Root)
	if h == nil {
		return nil
	}
	var out []Related
	for other, together := range h.pairs[file] {
		if together < minTogether {
			continue
		}
		if _, ok := m.byPath[other]; !ok {
			continue
		}
		out = append(out, Related{Path: other, Together: together, Commits: h.commits[file]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Together != out[j].Together {
			return out[i].Together > out[j].Together
		}
		return out[i].Path < out[j].Path
	})
	return out[:min(len(out), limit)]
}

// historyFor reads the history at the current HEAD, reusing the last read
// while HEAD has not moved.
func (p *project) historyFor(ctx context.Context, root string) *cochange {
	p.historyMu.Lock()
	defer p.historyMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := gitCommand(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	head := strings.TrimSpace(string(out))
	if p.history != nil && p.history.head == head {
		return p.history
	}
	log, err := gitCommand(ctx, root, "log", "--no-merges", "--relative", "-n", strconv.Itoa(historyCommits), "--name-only", "--pretty=format:%x1e")
	if err != nil {
		return nil
	}
	p.history = parseHistory(head, string(log))
	return p.history
}

// parseHistory counts, from git log output with each commit started by
// \x1e, how often each file and each pair of files changed.
func parseHistory(head, log string) *cochange {
	h := &cochange{head: head, commits: map[string]int{}, pairs: map[string]map[string]int{}}
	for _, commit := range strings.Split(log, "\x1e") {
		var files []string
		for _, line := range strings.Split(commit, "\n") {
			if line = strings.TrimSpace(line); line != "" && Category(LanguageOf(line)) == CategoryCode {
				files = append(files, line)
			}
		}
		if len(files) == 0 || len(files) > maxCommitFiles {
			continue
		}
		for _, a := range files {
			h.commits[a]++
			for _, b := range files {
				if a == b {
					continue
				}
				if h.pairs[a] == nil {
					h.pairs[a] = map[string]int{}
				}
				h.pairs[a][b]++
			}
		}
	}
	return h
}
