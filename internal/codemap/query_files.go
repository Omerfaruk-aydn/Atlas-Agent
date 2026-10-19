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

