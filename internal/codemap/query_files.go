package codemap

import (
	"fmt"
	"sort"
	"strings"
)

// Limits for the parts of an answer about documents and assets.
const (
	maxOverviewDocs   = 8
	maxListedProblems = 8
	maxListedAssets   = 5
)

// subsystem names fi's community, or "" for a file outside the code graph.
func (m *Map) subsystem(fi int) string {
	if len(m.communities) < 2 || m.community[fi] < 0 || m.community[fi] >= len(m.communities) {
		return ""
	}
	return m.communities[m.community[fi]].Label
}

// describeAsset is what the map measured of an asset: kind, format,
// dimensions and size.
func describeAsset(lang string, a *Asset) string {
	parts := []string{lang}
	if a.Format != "" {
		parts = append(parts, a.Format)
	}
	if a.Width > 0 && a.Height > 0 {
		parts = append(parts, fmt.Sprintf("%dx%d", a.Width, a.Height))
	}
	parts = append(parts, formatBytes(a.Bytes))
	return strings.Join(parts, ", ")
}

// pathUsers returns the links naming fi by path, ordered by file and line.
func (m *Map) pathUsers(fi int) []pathUse {
	uses := append([]pathUse(nil), m.pathIn[fi]...)
	sort.SliceStable(uses, func(i, j int) bool {
		a, b := uses[i], uses[j]
		if a.file != b.file {
			return m.Files[a.file].Path < m.Files[b.file].Path
		}
		return m.Files[a.file].Links[a.link].Line < m.Files[b.file].Links[b.link].Line
	})
	return uses
}

// describeUse is one line for a link naming a file: where and how.
func (m *Map) describeUse(u pathUse) string {
	f := m.Files[u.file]
	l := f.Links[u.link]
	return fmt.Sprintf("%s:%d %s %s", f.Path, l.Line, l.Kind, l.Target)
}

// referrers lists every file that depends on fi: by import, by use of its
// definitions or by naming its path.
func (m *Map) referrers(fi int) []int {
	seen := map[int]bool{}
	var out []int
	add := func(f int) {
		if f != fi && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, f := range m.importers(fi) {
		add(f)
	}
	for _, f := range m.importedBy[fi] {
		add(f)
	}
	for _, u := range m.pathIn[fi] {
		add(u.file)
	}
	first := m.firstSymbol[fi]
	for k := range m.Files[fi].Symbols {
		for _, lid := range m.in[first+k] {
			add(m.links[lid].File)
		}
	}
	sort.Slice(out, func(i, j int) bool { return m.Files[out[i]].Path < m.Files[out[j]].Path })
	return out
}

// reportMissing says whether a link whose target is not in the project is
// worth reporting. A path-like string in code may well be a file the
// program writes, and a test names files that do not exist on purpose.
func (m *Map) reportMissing(fi int, l FileLink) bool {
	// A command runs in a working directory the map does not know.
	return l.Kind != LinkNames && l.Kind != LinkRuns && !isTestPath(m.Files[fi].Path)
}

// linkProblem is a link to a missing file or a missing heading.
type linkProblem struct {
	use    pathUse
	anchor bool
}

// linkProblems lists the links whose file or heading the map does not
// find, optionally only those in one file.
func (m *Map) linkProblems(only int) []linkProblem {
	var out []linkProblem
	for fi, targets := range m.paths {
		if only >= 0 && fi != only {
			continue
		}
		for k, t := range targets {
			l := m.Files[fi].Links[k]
			switch {
			case t.brokenAnchor:
				out = append(out, linkProblem{pathUse{fi, k}, true})
			case len(t.files) == 0 && !t.pattern && !t.outside && !t.fixture && t.unread == "" && m.reportMissing(fi, l):
				out = append(out, linkProblem{pathUse{fi, k}, false})
			}
		}
	}
	return out
}

func (m *Map) describeProblem(p linkProblem) string {
	if p.anchor {
		return m.describeUse(p.use) + " (no such heading)"
	}
	return m.describeUse(p.use) + " (not found)"
}

// unusedAssets lists the assets no file names by path or imports.
func (m *Map) unusedAssets() []int {
	var out []int
	for fi, f := range m.Files {
		if f.Asset == nil || len(m.pathIn[fi]) > 0 || len(m.importedBy[fi]) > 0 {
			continue
		}
		out = append(out, fi)
	}
	return out
}

// overviewDocs summarizes the documentation: the most linked documents
// with their size in sections.
func (m *Map) overviewDocs(b *strings.Builder) {
	var docs []int
	sections := 0
	for fi, f := range m.Files {
		if Category(f.Language) != CategoryDoc {
			continue
		}
		docs = append(docs, fi)
		sections += m.countKind(fi, "section")
	}
	if len(docs) == 0 {
		return
	}
	inbound := func(fi int) int { return len(m.referrers(fi)) }
	counts := make(map[int]int, len(docs))
	for _, fi := range docs {
		counts[fi] = inbound(fi)
	}
	sort.SliceStable(docs, func(i, j int) bool {
		a, b := docs[i], docs[j]
		if counts[a] != counts[b] {
			return counts[a] > counts[b]
		}
		if da, db := strings.Count(m.Files[a].Path, "/"), strings.Count(m.Files[b].Path, "/"); da != db {
			return da < db
		}
		return m.Files[a].Path < m.Files[b].Path
	})
	fmt.Fprintf(b, "\nDocuments: %s, %s.\n", plural(len(docs), "file"), plural(sections, "section"))
	for _, fi := range docs[:min(len(docs), maxOverviewDocs)] {
		f := m.Files[fi]
		detail := []string{plural(m.countKind(fi, "section"), "section")}
		if n := counts[fi]; n > 0 {
			detail = append(detail, "named by "+plural(n, "file"))
		}
		if n := m.codeMentions(fi); n > 0 {
			detail = append(detail, "mentions "+plural(n, "definition"))
		}
		title := ""
		if s := m.firstSection(fi); s != "" {
			title = " " + s + ":"
		}
		fmt.Fprintf(b, "  %s:%s %s\n", f.Path, title, strings.Join(detail, ", "))
	}
	if rest := len(docs) - maxOverviewDocs; rest > 0 {
		fmt.Fprintf(b, "  … %d more\n", rest)
	}
}

func (m *Map) countKind(fi int, kind string) int {
	n := 0
	for _, s := range m.Files[fi].Symbols {
		if s.Kind == kind {
			n++
		}
	}
	return n
}

// firstSection is a document's title: its first heading.
func (m *Map) firstSection(fi int) string {
	for _, s := range m.Files[fi].Symbols {
		if s.Kind == "section" {
			return s.Signature
		}
	}
	return ""
}

// codeMentions counts the distinct code definitions a file links to.
func (m *Map) codeMentions(fi int) int {
	seen := map[int]bool{}
	for _, l := range m.links {
		if l.File == fi && m.code[m.symbols[l.To].file] {
			seen[l.To] = true
		}
	}
	return len(seen)
}

// explainMentions lists the code definitions a document names, each with
// the first line naming it.
func (m *Map) explainMentions(b *strings.Builder, fi int) {
	first := map[int]Link{}
	var order []int
	for _, l := range m.links {
		if l.File != fi || !m.code[m.symbols[l.To].file] {
			continue
		}
		if prev, ok := first[l.To]; !ok {
			order = append(order, l.To)
			first[l.To] = l
		} else if l.Line < prev.Line {
			first[l.To] = l
		}
	}
	if len(order) == 0 {
		return
	}
	sort.SliceStable(order, func(i, j int) bool { return first[order[i]].Line < first[order[j]].Line })
	fmt.Fprintf(b, "Mentions in the code (%d):\n", len(order))
	for _, id := range order[:min(len(order), maxListedLinks)] {
		s := m.symbol(id)
		l := first[id]
		fmt.Fprintf(b, "  L%d %s %s at %s:%d [%s]\n", l.Line, s.Kind, qualifiedName(s), m.Files[m.symbols[id].file].Path, s.Line, l.Confidence)
	}
	if len(order) > maxListedLinks {
		fmt.Fprintf(b, "  … %d more\n", len(order)-maxListedLinks)
	}
}

// overviewAssets summarizes images, fonts, media, models and data: counts,
// total size, the largest, and those nothing names.
func (m *Map) overviewAssets(b *strings.Builder) {
	var assets []int
	kinds := map[string]int{}
	var total int64
	for fi, f := range m.Files {
		if f.Asset == nil {
			continue
		}
		assets = append(assets, fi)
		kinds[f.Language]++
		total += f.Asset.Bytes
	}
	if len(assets) == 0 {
		return
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		if kinds[names[i]] != kinds[names[j]] {
			return kinds[names[i]] > kinds[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = fmt.Sprintf("%s %d", k, kinds[k])
	}
	fmt.Fprintf(b, "\nAssets: %s, %s (%s).\n", plural(len(assets), "file"), formatBytes(total), strings.Join(parts, ", "))
	sort.SliceStable(assets, func(i, j int) bool { return m.Files[assets[i]].Asset.Bytes > m.Files[assets[j]].Asset.Bytes })
	var largest []string
	for _, fi := range assets[:min(len(assets), maxListedAssets)] {
		f := m.Files[fi]
		largest = append(largest, fmt.Sprintf("%s (%s)", f.Path, describeAsset(f.Language, f.Asset)))
	}
	b.WriteString("  Largest: " + strings.Join(largest, "; ") + "\n")
	if unused := m.unusedAssets(); len(unused) > 0 {
		fmt.Fprintf(b, "  Named by no file (%d; may still be loaded by a computed name or a directory listing): %s\n", len(unused), m.pathList(unused, maxListedAssets))
	}
}

// overviewProblems lists links whose target the map does not find.
func (m *Map) overviewProblems(b *strings.Builder) {
	problems := m.linkProblems(-1)
	if len(problems) == 0 {
		return
	}
	anchors := 0
	for _, p := range problems {
		if p.anchor {
			anchors++
		}
	}
	fmt.Fprintf(b, "\nBroken links: %d to missing files, %d to missing headings (a target may be generated or served from elsewhere):\n", len(problems)-anchors, anchors)
	for _, p := range problems[:min(len(problems), maxListedProblems)] {
		b.WriteString("  " + m.describeProblem(p) + "\n")
	}
	if rest := len(problems) - maxListedProblems; rest > 0 {
		fmt.Fprintf(b, "  … %d more\n", rest)
	}
}

// explainLinks lists the files fi names by path and what each resolved to.
func (m *Map) explainLinks(b *strings.Builder, fi int) {
	f := m.Files[fi]
	if len(f.Links) == 0 {
		return
	}
	fmt.Fprintf(b, "Names by path (%d):\n", len(f.Links))
	for k, l := range f.Links {
		if k == maxListedLinks {
			fmt.Fprintf(b, "  … %d more\n", len(f.Links)-maxListedLinks)
			break
		}
		t := m.paths[fi][k]
		var where string
		switch {
		case t.fixture:
			where = "(test data; not resolved)"
		case t.outside:
			where = "(outside the project)"
		case t.unread != "":
			where = "-> " + t.unread + " (not mapped)"
		case t.pattern && len(t.files) > 0:
			where = fmt.Sprintf("-> %s such as %s", plural(len(t.files), "file"), m.Files[t.files[0]].Path)
		case t.pattern:
			where = "(a computed name; matches no file)"
		case len(t.files) == 0:
			where = "(not found)"
		case t.files[0] == fi:
			where = "(this file)"
		default:
			where = "-> " + m.Files[t.files[0]].Path
		}
		switch {
		case t.brokenAnchor:
			where += " (no such heading)"
		case t.section >= 0:
			where += fmt.Sprintf(" at line %d", m.symbol(t.section).Line)
		}
		fmt.Fprintf(b, "  L%d %s %s %s\n", l.Line, l.Kind, l.Target, where)
	}
}

// explainPathUsers lists the links naming fi by path.
func (m *Map) explainPathUsers(b *strings.Builder, fi int) {
	uses := m.pathUsers(fi)
	if len(uses) == 0 {
		return
	}
	fmt.Fprintf(b, "Named by path in (%d):\n", len(uses))
	for _, u := range uses[:min(len(uses), maxListedLinks)] {
		b.WriteString("  " + m.describeUse(u) + "\n")
	}
	if len(uses) > maxListedLinks {
		fmt.Fprintf(b, "  … %d more\n", len(uses)-maxListedLinks)
	}
}

// domainSymbols returns the definitions outside code named text, or
// Container.text; a SQL name matches in any case.
func (m *Map) domainSymbols(text string) []int {
	var out []int
	for id := range m.symbols {
		if m.symDomain[id] == domainCode {
			continue
		}
		s := m.symbol(id)
		if s.Name == text || s.Container != "" && s.Container+"."+s.Name == text ||
			m.symDomain[id] == domainSQL && strings.EqualFold(s.Name, text) {
			out = append(out, id)
		}
	}
	return out
}

