package codemap

import (
	"context"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

// Output limits keep each answer a fraction of reading the files.
const (
	maxFindResults   = 30
	maxListedLinks   = 25
	maxCallerLines   = 60
	maxPathDepth     = 10
	defaultBudget    = 2000
	maxBudget        = 8000
	charsPerToken    = 4
	maxOverviewFiles = 60
)

// Stats summarizes the map.
func (m *Map) Stats() (files, symbols int, byLanguage map[string]int, byConfidence map[Confidence]int) {
	byLanguage = map[string]int{}
	byConfidence = map[Confidence]int{}
	for _, f := range m.Files {
		byLanguage[f.Language]++
		symbols += len(f.Symbols)
	}
	for _, l := range m.links {
		byConfidence[l.Confidence]++
	}
	return len(m.Files), symbols, byLanguage, byConfidence
}

// Overview describes the project in about budget tokens: its languages,
// its subsystems, and its most depended-on files with their main
// definitions, most important first.
func (m *Map) Overview(budget int) string {
	if budget <= 0 {
		budget = defaultBudget
	}
	budget = min(budget, maxBudget)
	limit := budget * charsPerToken
	var b strings.Builder
	files, symbols, langs, conf := m.Stats()
	type count struct {
		name string
		n    int
	}
	var langList []count
	for name, n := range langs {
		langList = append(langList, count{name, n})
	}
	sort.Slice(langList, func(i, j int) bool {
		if langList[i].n != langList[j].n {
			return langList[i].n > langList[j].n
		}
		return langList[i].name < langList[j].name
	})
	byCategory := map[string][]string{}
	for _, c := range langList {
		cat := Category(c.name)
		byCategory[cat] = append(byCategory[cat], fmt.Sprintf("%s %d", c.name, c.n))
	}
	var parts []string
	for _, cat := range []struct{ key, label string }{{CategoryCode, "code"}, {CategoryDoc, "docs"}, {CategoryAsset, "assets"}} {
		list := byCategory[cat.key]
		switch {
		case len(list) == 0:
		case len(byCategory) == 1:
			parts = append(parts, strings.Join(list, ", "))
		default:
			parts = append(parts, cat.label+": "+strings.Join(list, ", "))
		}
	}
	fmt.Fprintf(&b, "Code map of %s: %d files (%s), %d definitions.\n", path.Base(m.Root), files, strings.Join(parts, "; "), symbols)
	fmt.Fprintf(&b, "Links between them: %d resolved, %d inferred, %d ambiguous.\n", conf[Resolved], conf[Inferred], conf[Ambiguous])
	if m.Truncated {
		fmt.Fprintf(&b, "The project has more than %d source files; only the first %d by path are mapped.\n", maxFiles, maxFiles)
	}
	if len(m.Skipped) > 0 {
		fmt.Fprintf(&b, "Skipped %d files (%s).\n", len(m.Skipped), strings.Join(m.Skipped[:min(len(m.Skipped), 3)], "; "))
	}
	if files == 0 {
		b.WriteString("No source files found.\n")
		return b.String()
	}

	if len(m.communities) > 1 {
		b.WriteString("\nSubsystems (files grouped by how they use each other):\n")
		for i, c := range m.communities[:min(len(m.communities), 12)] {
			top := m.topFiles(c.Files, 3)
			fmt.Fprintf(&b, "%d. %s: %d files; central: %s\n", i+1, c.Label, len(c.Files), strings.Join(top, ", "))
		}
		if extra := len(m.communities) - 12; extra > 0 {
			fmt.Fprintf(&b, "… %d smaller ones.\n", extra)
		}
	}

	if entries := m.entryPoints(); len(entries) > 0 {
		b.WriteString("\nEntry points: " + strings.Join(entries, ", ") + "\n")
	}

	b.WriteString("\nMost depended-on files and their main definitions:\n")
	order := m.filesByRank()
	shown := 0
	var bare, tests []string
	// Documents, assets and broken links get the last quarter of the
	// budget, so a large codebase does not crowd them out.
	codeLimit := limit
	if len(byCategory) > 1 {
		codeLimit = limit * 3 / 4
	}
	codeFiles := 0
	for _, fi := range order {
		f := m.Files[fi]
		if !m.code[fi] {
			continue
		}
		codeFiles++
		if isTestPath(f.Path) {
			tests = append(tests, f.Path)
			continue
		}
		if b.Len() > codeLimit || shown >= maxOverviewFiles {
			continue
		}
		shown++
		top := m.topSymbols(fi, 4)
		if len(top) == 0 {
			bare = append(bare, f.Path)
			continue
		}
		fmt.Fprintf(&b, "%s (%s, %d lines%s)\n", f.Path, f.Language, f.Lines, m.usedBy(fi))
		for _, id := range top {
			s := m.symbol(id)
			fmt.Fprintf(&b, "  L%-5d %s\n", s.Line, s.Signature)
		}
	}
	if len(bare) > 0 {
		b.WriteString("Files without definitions of their own: " + strings.Join(bare, ", ") + "\n")
	}
	if rest := codeFiles - len(tests) - shown; rest > 0 {
		fmt.Fprintf(&b, "… %d more files; find a name or path to see them.\n", rest)
	}
	if len(tests) > 0 {
		fmt.Fprintf(&b, "Test files: %d (%s)\n", len(tests), strings.Join(testDirs(tests), ", "))
	}
	// Documents, assets and broken links come after the code, each while
	// the budget lasts.
	for _, section := range []func(*strings.Builder){m.overviewDocs, m.overviewAssets, m.overviewProblems} {
		if b.Len() > limit {
			break
		}
		section(&b)
	}
	return b.String()
}

// usedBy says how many files depend on fi, tests counted apart.
func (m *Map) usedBy(fi int) string {
	code, tests := 0, 0
	for _, user := range m.importers(fi) {
		if isTestPath(m.Files[user].Path) {
			tests++
		} else {
			code++
		}
	}
	switch {
	case code == 0 && tests == 0:
		return ""
	case tests == 0:
		return ", used by " + plural(code, "file")
	case code == 0:
		return ", used by " + plural(tests, "test file")
	}
	return ", used by " + plural(code, "file") + " and " + plural(tests, "test file")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// testDirs names the directories holding test files, most first.
func testDirs(tests []string) []string {
	counts := map[string]int{}
	for _, t := range tests {
		counts[path.Dir(t)]++
	}
	dirs := make([]string, 0, len(counts))
	for d := range counts {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if counts[dirs[i]] != counts[dirs[j]] {
			return counts[dirs[i]] > counts[dirs[j]]
		}
		return dirs[i] < dirs[j]
	})
	return dirs[:min(len(dirs), 5)]
}

// filesByRank orders files by importance, then rank, then path.
func (m *Map) filesByRank() []int {
	order := make([]int, len(m.Files))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if m.fileScore[a] != m.fileScore[b] {
			return m.fileScore[a] > m.fileScore[b]
		}
		if m.fileRank[a] != m.fileRank[b] {
			return m.fileRank[a] > m.fileRank[b]
		}
		return m.Files[a].Path < m.Files[b].Path
	})
	return order
}

func (m *Map) topFiles(files []int, n int) []string {
	sorted := append([]int(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return m.fileRank[sorted[i]] > m.fileRank[sorted[j]] })
	var out []string
	for _, fi := range sorted[:min(len(sorted), n)] {
		out = append(out, m.Files[fi].Path)
	}
	return out
}

// topSymbols returns a file's most used top-level definitions.
func (m *Map) topSymbols(fi, n int) []int {
	first := m.firstSymbol[fi]
	var ids []int
	for k, s := range m.Files[fi].Symbols {
		if s.Kind == "namespace" || s.Kind == "impl" || m.nested[first+k] {
			continue
		}
		ids = append(ids, first+k)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := m.symbolScore[ids[i]], m.symbolScore[ids[j]]
		if a != b {
			return a > b
		}
		sa, sb := m.symbol(ids[i]), m.symbol(ids[j])
		if sa.Exported != sb.Exported {
			return sa.Exported
		}
		return sa.Line < sb.Line
	})
	return ids[:min(len(ids), n)]
}

