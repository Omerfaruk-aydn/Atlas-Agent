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

// importers lists the files that depend on fi.
func (m *Map) importers(fi int) []int {
	var out []int
	for key := range m.fileLinks {
		if key[1] == fi {
			out = append(out, key[0])
		}
	}
	sort.Ints(out)
	return out
}

func (m *Map) dependencies(fi int) []int {
	var out []int
	for key := range m.fileLinks {
		if key[0] == fi {
			out = append(out, key[1])
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return m.fileLinks[[2]int{fi, out[i]}] > m.fileLinks[[2]int{fi, out[j]}]
	})
	return out
}

// entryNames are file names programs usually start from.
var entryNames = words("main index app server cli manage __main__ program Program Main")

// entryPoints are where the program starts: files defining main, or named
// like an entry point that nothing imports. Without either, the files that
// depend on others but nothing depends on.
func (m *Map) entryPoints() []string {
	used := map[int]bool{}
	uses := map[int]float64{}
	for key, w := range m.fileLinks {
		used[key[1]] = true
		uses[key[0]] += w
	}
	var out []int
	for fi, f := range m.Files {
		if isTestPath(f.Path) || !m.code[fi] {
			continue
		}
		base := strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path))
		definesMain := slices.ContainsFunc(f.Symbols, func(s Symbol) bool { return s.Name == "main" && s.Kind == "function" })
		if definesMain || entryNames[base] && !used[fi] {
			out = append(out, fi)
		}
	}
	if len(out) == 0 {
		for fi, f := range m.Files {
			if !used[fi] && uses[fi] > 0 && !isTestPath(f.Path) && m.code[fi] {
				out = append(out, fi)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return uses[out[i]] > uses[out[j]] })
	var names []string
	for _, fi := range out[:min(len(out), 8)] {
		names = append(names, m.Files[fi].Path)
	}
	return names
}

func isTestPath(p string) bool {
	lower := strings.ToLower(p)
	return strings.Contains(lower, "test") || strings.Contains(lower, "spec") || strings.Contains(lower, "__tests__")
}

// Find lists definitions and files whose name matches text: exact names
// first, then prefixes, then substrings.
func (m *Map) Find(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Give a name or part of one to find."
	}
	lower := strings.ToLower(text)
	type hit struct {
		id   int
		tier int
	}
	var hits []hit
	for id := range m.symbols {
		s := m.symbol(id)
		name := strings.ToLower(s.Name)
		qualified := strings.ToLower(s.Container + "." + s.Name)
		tier := -1
		switch {
		case s.Name == text || s.Container+"."+s.Name == text:
			tier = 0
		case name == lower || qualified == lower:
			tier = 1
		case strings.HasPrefix(name, lower):
			tier = 2
		case strings.Contains(name, lower) || strings.Contains(qualified, lower):
			tier = 3
		}
		if tier >= 0 {
			hits = append(hits, hit{id, tier})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].tier != hits[j].tier {
			return hits[i].tier < hits[j].tier
		}
		return m.symbolScore[hits[i].id] > m.symbolScore[hits[j].id]
	})
	var b strings.Builder
	for _, h := range hits[:min(len(hits), maxFindResults)] {
		b.WriteString(m.describe(h.id) + "\n")
	}
	if len(hits) > maxFindResults {
		fmt.Fprintf(&b, "… %d more; use a longer name.\n", len(hits)-maxFindResults)
	}
	var files []string
	for _, f := range m.Files {
		if strings.Contains(strings.ToLower(f.Path), lower) {
			files = append(files, f.Path)
		}
	}
	if len(files) > 0 {
		fmt.Fprintf(&b, "Files: %s", strings.Join(files[:min(len(files), 15)], ", "))
		if len(files) > 15 {
			fmt.Fprintf(&b, " … %d more", len(files)-15)
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return fmt.Sprintf("Nothing named like %q in the map. It may be defined in a dependency, generated at runtime, or in a language construct the map does not read; grep for it.", text)
	}
	return b.String()
}

// describe is one line for a definition: kind, name, place, signature.
func (m *Map) describe(id int) string {
	s := m.symbol(id)
	f := m.Files[m.symbols[id].file]
	return fmt.Sprintf("%s %s  %s:%d  %s", s.Kind, qualifiedName(s), f.Path, s.Line, s.Signature)
}

func qualifiedName(s *Symbol) string {
	if s.Container != "" {
		return s.Container + "." + s.Name
	}
	return s.Name
}

// target is what a query names: definitions or a file.
type target struct {
	symbols []int
	file    int
}

// resolveTarget finds what text names: a file path, path:Name, a
// Container.Name or a Name. An ambiguous text returns an error listing the
// candidates.
func (m *Map) resolveTarget(text string) (target, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return target{file: -1}, fmt.Errorf("name a definition or a file")
	}
	if fi, ok := m.findFile(text); ok {
		return target{file: fi}, nil
	}
	if file, name, ok := strings.Cut(text, ":"); ok && LanguageOf(file) != "" {
		fi, found := m.findFile(file)
		if !found {
			return target{file: -1}, fmt.Errorf("no mapped file %s", file)
		}
		var ids []int
		for _, id := range m.byName[name] {
			if m.symbols[id].file == fi {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return target{file: -1}, fmt.Errorf("%s defines nothing named %s", m.Files[fi].Path, name)
		}
		return target{symbols: ids, file: -1}, nil
	}
	ids := m.byQualified[text]
	if len(ids) == 0 {
		ids = m.byName[text]
	}
	// Definitions outside code (a CSS class, a table, a heading, a script)
	// are indexed by domain; a query names them as written.
	ids = append(append([]int(nil), ids...), m.domainSymbols(text)...)
	if len(ids) == 0 {
		return target{file: -1}, fmt.Errorf("nothing named %s in the map; try find", text)
	}
	return target{symbols: ids, file: -1}, nil
}

// findFile finds a file by path or by a unique path suffix.
func (m *Map) findFile(text string) (int, bool) {
	text = strings.TrimPrefix(path.Clean(strings.ReplaceAll(text, "\\", "/")), "./")
	if fi, ok := m.byPath[text]; ok {
		return fi, true
	}
	if LanguageOf(text) == "" {
		return -1, false
	}
	found := -1
	for fi, f := range m.Files {
		if strings.HasSuffix(f.Path, "/"+text) {
			if found >= 0 {
				return -1, false
			}
			found = fi
		}
	}
	return found, found >= 0
}

// Explain describes a definition or a file and how it connects to the
// rest: where it is, what uses it, what it uses, and for a file what it
// imports, what imports it and what changes with it.
func (m *Map) Explain(ctx context.Context, text string) string {
	t, err := m.resolveTarget(text)
	if err != nil {
		return err.Error()
	}
	if t.file >= 0 {
		return m.explainFile(ctx, t.file)
	}
	var b strings.Builder
	if len(t.symbols) > 3 {
		fmt.Fprintf(&b, "%d definitions are named %s; showing the 3 most used. Name one as path:%s or Container.%s.\n\n", len(t.symbols), text, lastName(text), lastName(text))
	}
	ids := append([]int(nil), t.symbols...)
	sort.SliceStable(ids, func(i, j int) bool { return m.symbolScore[ids[i]] > m.symbolScore[ids[j]] })
	for k, id := range ids[:min(len(ids), 3)] {
		if k > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.explainSymbol(id))
	}
	return b.String()
}

