package codemap

import (
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// pathTarget is what a FileLink resolved to.
type pathTarget struct {
	// files are the files the link reaches; none means its target is
	// missing. A link with ${...} in it may reach several.
	files   []int
	pattern bool
	// anchor is the #fragment; section the heading it names in a
	// document, or -1. brokenAnchor is set when the document has no such
	// heading.
	anchor       string
	section      int
	brokenAnchor bool
	// fixture marks a path a test names, which is left unresolved.
	fixture bool
	// outside marks an absolute path that is not in the project.
	outside bool
	// unread is the file or directory the link names when the map does
	// not read it (a JSON file, a folder of them).
	unread string
}

// pathUse locates a link: Files[file].Links[link].
type pathUse struct{ file, link int }

// maxPatternMatches bounds what one ${...} path reaches.
const maxPatternMatches = 200

// resolvePaths resolves every FileLink. A link between two code files is
// also a dependency, weaker than an import.
func (m *Map) resolvePaths() {
	m.paths = make([][]pathTarget, len(m.Files))
	m.pathIn = map[int][]pathUse{}
	m.anchorIn = map[int][]pathUse{}
	for fi, f := range m.Files {
		m.paths[fi] = make([]pathTarget, len(f.Links))
		fixtures := isTestPath(f.Path)
		for k, l := range f.Links {
			if fixtures && l.Kind == LinkNames {
				// A test names files of the trees it builds, not the
				// project's own.
				m.paths[fi][k] = pathTarget{section: -1, fixture: true}
				continue
			}
			t := m.resolvePath(fi, l.Target)
			m.paths[fi][k] = t
			for _, target := range t.files {
				if target == fi {
					continue
				}
				m.pathIn[target] = append(m.pathIn[target], pathUse{fi, k})
				if m.code[fi] && m.code[target] && !t.pattern {
					m.fileLinks[[2]int{fi, target}] += 0.5
				}
			}
			if t.section >= 0 {
				m.anchorIn[t.section] = append(m.anchorIn[t.section], pathUse{fi, k})
			}
		}
	}
}

// resolvePath finds the file a path written in fi means.
func (m *Map) resolvePath(fi int, target string) pathTarget {
	t := pathTarget{section: -1}
	file, frag, _ := strings.Cut(target, "#")
	if q := strings.IndexByte(file, '?'); q >= 0 {
		file = file[:q]
	}
	if decoded, err := url.PathUnescape(file); err == nil {
		file = decoded
	}
	t.anchor = frag
	// direction-%d.png and frame_{index}.png are names computed like
	// ${...} ones.
	file = placeholders(file)
	if isDrivePath(file) {
		// D:/project/src/x.go: a link to the project's own file through
		// the author's disk, or to somewhere else on it.
		rel, ok := m.projectRelative(file)
		if !ok {
			t.outside = true
			return t
		}
		file = "/" + rel
	}
	switch {
	case file == "":
		t.files = []int{fi}
	case strings.Contains(file, "${"):
		t.pattern = true
		t.files = m.matchPattern(file)
	case strings.ContainsAny(file, "*?["):
		t.pattern = true
		t.files = m.matchGlob(fi, file)
	default:
		if found := m.findPath(fi, file); found >= 0 {
			t.files = []int{found}
		} else if under := m.underDir(fi, file); len(under) > 0 {
			// A directory: everything in it.
			t.pattern = true
			t.files = under
		} else if p, ok := m.existsUnread(fi, file); ok {
			t.unread = p
		}
	}
	if frag != "" && len(t.files) == 1 {
		if target := m.Files[t.files[0]]; Category(target.Language) == CategoryDoc {
			t.section = m.sectionBySlug(t.files[0], frag)
			t.brokenAnchor = t.section < 0
		}
	}
	return t
}

// findPath finds a file by a path relative to the referring file, to the
// project, or to a web root (public, static), else by a unique ending.
func (m *Map) findPath(fi int, p string) int {
	p = strings.ReplaceAll(p, "\\", "/")
	dir := path.Dir(m.Files[fi].Path)
	cands := pathCandidates(dir, p)
	if cands == nil {
		return -1
	}
	try := func(c string) int {
		if i, ok := m.byPath[c]; ok {
			return i
		}
		return -1
	}
	for _, c := range cands {
		if i := try(c); i >= 0 {
			return i
		}
	}
	if path.Ext(p) == "" {
		for _, ext := range []string{".md", ".mdx", ".tsx", ".ts", ".jsx", ".js", ".vue", ".svelte", ".html", ".rst", ".adoc"} {
			for _, c := range cands {
				if i := try(c + ext); i >= 0 {
					return i
				}
			}
		}
	}
	// By its ending: images/sun.png matches public/textures/images/sun.png
	// when it is the only file that ends so, or the nearest.
	var parts []string
	for _, s := range strings.Split(path.Clean(p), "/") {
		if s != ".." && s != "." && s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return -1
	}
	matches := m.pathSuffix[strings.Join(parts, "/")]
	switch len(matches) {
	case 0:
		return -1
	case 1:
		return matches[0]
	}
	best, bestScore, tie := -1, -1, false
	for _, c := range matches {
		score := commonPrefix(dir, path.Dir(m.Files[c].Path))
		switch {
		case score > bestScore:
			best, bestScore, tie = c, score, false
		case score == bestScore:
			tie = true
		}
	}
	if tie {
		return -1
	}
	return best
}

// pathCandidates lists, cleaned and inside the project, the paths p
// written in a file of dir can mean: relative to dir, to the project, or
// to a web root (public, static). It is nil for a package path (~pkg).
func pathCandidates(dir, p string) []string {
	var cands []string
	switch {
	case strings.HasPrefix(p, "res://"):
		cands = []string{strings.TrimPrefix(p, "res://")}
	case strings.HasPrefix(p, "/"):
		r := strings.TrimPrefix(p, "/")
		cands = []string{r, "public/" + r, "static/" + r, "src/" + r, "assets/" + r, "app/" + r, path.Join(dir, r)}
	case strings.HasPrefix(p, "@/") || strings.HasPrefix(p, "~/"):
		r := p[2:]
		cands = []string{"src/" + r, r}
	case strings.HasPrefix(p, "~"):
		// A package in node_modules.
		return nil
	default:
		cands = []string{path.Join(dir, p), p, "public/" + p, "static/" + p, "src/" + p, "assets/" + p}
	}
	out := cands[:0]
	for _, c := range cands {
		if c = path.Clean(c); c != ".." && !strings.HasPrefix(c, "../") {
			out = append(out, c)
		}
	}
	return out
}

// existsUnread reports whether p, written in fi, names a project file
// the map does not read, or a directory.
func (m *Map) existsUnread(fi int, p string) (string, bool) {
	for _, c := range pathCandidates(path.Dir(m.Files[fi].Path), strings.ReplaceAll(p, "\\", "/")) {
		if m.others[c] || m.folders[c] || c == "." {
			return c, true
		}
	}
	return "", false
}

// commonPrefix counts the leading directories two paths share.
func commonPrefix(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}

// matchPattern returns the files a path with ${...} parts can be: each
// part any one name.
func (m *Map) matchPattern(p string) []int {
	literal := strings.Trim(stripTemplates(p), "x./")
	if len(strings.TrimSuffix(literal, path.Ext(literal))) < 2 {
		// ${name}.png says too little to be worth linking.
		return nil
	}
	var b strings.Builder
	b.WriteString(`(?:^|/)`)
	rest := strings.TrimLeft(path.Clean(p), "./")
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			b.WriteString(regexp.QuoteMeta(rest))
			break
		}
		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			return nil
		}
		b.WriteString(regexp.QuoteMeta(rest[:start]))
		b.WriteString(`[^/]+`)
		rest = rest[start+end+1:]
	}
	b.WriteString(`$`)
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	ext := strings.ToLower(path.Ext(p))
	var out []int
	for fi, f := range m.Files {
		if strings.ToLower(path.Ext(f.Path)) == ext && re.MatchString(f.Path) {
			out = append(out, fi)
			if len(out) == maxPatternMatches {
				break
			}
		}
	}
	return out
}

// placeholderRe matches a printf verb or a {name} format field in a path.
var placeholderRe = regexp.MustCompile(`%[-+# 0]*\d*(?:\.\d+)?[dsvqxXfg]|\{[A-Za-z_]?\w*(?::[^{}/]*)?\}`)

// placeholders rewrites printf verbs and {name} fields as ${x}, leaving
// ${...} as it is.
func placeholders(p string) string {
	if !strings.ContainsAny(p, "%{") {
		return p
	}
	locs := placeholderRe.FindAllStringIndex(p, -1)
	if locs == nil {
		return p
	}
	var b strings.Builder
	last := 0
	for _, l := range locs {
		if p[l[0]] == '{' && l[0] > 0 && p[l[0]-1] == '$' {
			continue
		}
		b.WriteString(p[last:l[0]])
		b.WriteString("${x}")
		last = l[1]
	}
	b.WriteString(p[last:])
	return b.String()
}

// isDrivePath reports whether p is an absolute Windows path, C:/x or C:\x.
func isDrivePath(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\') &&
		(p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}

// projectRelative turns an absolute path inside the project into one
// relative to its root.
func (m *Map) projectRelative(p string) (string, bool) {
	root := m.Root
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	root = strings.TrimSuffix(strings.ReplaceAll(root, "\\", "/"), "/") + "/"
	p = strings.ReplaceAll(p, "\\", "/")
	if len(p) < len(root) || !strings.EqualFold(p[:len(root)], root) {
		return "", false
	}
	return p[len(root):], true
}

