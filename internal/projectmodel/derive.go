package projectmodel

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codemap"
)

// Limits keep the stored model bounded on any project.
const (
	maxExports        = 6
	maxTestsPerSource = 12
	maxStemMatches    = 3
	maxImportFallback = 20
)

// nonSourceLanguages are code-category languages that describe structure,
// styling, data or build steps rather than a program. They are linked into
// the dependency graph but never form a module of their own.
var nonSourceLanguages = map[string]bool{
	"css": true, "scss": true, "less": true, "html": true, "template": true,
	"sql": true, "graphql": true, "protobuf": true, "thrift": true,
	"prisma": true, "openapi": true, "terraform": true, "npm": true,
	"make": true, "just": true, "taskfile": true, "docker": true,
	"compose": true, "workflow": true, "gitlab-ci": true, "pyproject": true,
	"cargo": true, "godot": true, "notebook": true,
}

// derived is what is computed from the code map alone.
type derived struct {
	modules []Module
	entries []Entry
	edges   map[string][]string
	testMap map[string][]TestRef
	langs   map[string]int
}

// slashDir is the directory of a slash-separated path, "." for none.
func slashDir(p string) string { return path.Dir(p) }

// isSource reports whether f is a program source file (not a test).
func isSource(m *codemap.Map, f *codemap.File) bool {
	return m.IsCodeFile(f.Path) && !nonSourceLanguages[f.Language] && !IsTestFile(f.Path)
}

// IsTestFile reports whether a path is a test by the usual naming and
// location conventions of the common ecosystems.
func IsTestFile(p string) bool {
	lower := strings.ToLower(p)
	base := path.Base(lower)
	ext := path.Ext(base)
	name := strings.TrimSuffix(base, ext)
	switch {
	case strings.HasSuffix(name, "_test"), strings.HasSuffix(name, ".test"),
		strings.HasSuffix(name, ".spec"), strings.HasSuffix(name, "_spec"),
		strings.HasPrefix(name, "test_"), strings.HasSuffix(name, "-test"),
		strings.HasSuffix(name, "-spec"):
		return true
	case (ext == ".java" || ext == ".kt" || ext == ".cs" || ext == ".scala" || ext == ".swift") &&
		(strings.HasSuffix(name, "test") || strings.HasSuffix(name, "tests") || strings.HasSuffix(name, "spec")) &&
		len(name) > 4:
		return true
	}
	for _, seg := range strings.Split(path.Dir(lower), "/") {
		if seg == "__tests__" || seg == "tests" || seg == "test" || seg == "spec" || seg == "specs" {
			return ext != "" && ext != ".md" && ext != ".json" && ext != ".txt"
		}
	}
	return false
}

// testStem returns the base name of the source file a test is named after,
// or "" when the name does not say.
func testStem(p string) string {
	base := path.Base(p)
	ext := path.Ext(base)
	name := strings.TrimSuffix(base, ext)
	lower := strings.ToLower(name)
	for _, suffix := range []string{"_test", ".test", ".spec", "_spec", "-test", "-spec"} {
		if strings.HasSuffix(lower, suffix) {
			return strings.ToLower(name[:len(name)-len(suffix)])
		}
	}
	if strings.HasPrefix(lower, "test_") {
		return lower[len("test_"):]
	}
	for _, suffix := range []string{"tests", "test", "spec"} {
		if strings.HasSuffix(lower, suffix) && len(lower) > len(suffix) {
			return lower[:len(lower)-len(suffix)]
		}
	}
	return ""
}

// derive computes modules, entry points, dependency edges and the test map
// from the code map. It is a pure function of the map and the project's
// manifest, cheap enough to rerun on every refresh.
func derive(m *codemap.Map, root string) derived {
	d := derived{edges: map[string][]string{}, testMap: map[string][]TestRef{}, langs: map[string]int{}}

	sources := map[string]*codemap.File{}
	var tests []string
	for _, f := range m.Files {
		switch {
		case isSource(m, f):
			sources[f.Path] = f
			d.langs[f.Language]++
		case m.IsCodeFile(f.Path) && !nonSourceLanguages[f.Language] && IsTestFile(f.Path):
			tests = append(tests, f.Path)
		}
	}
	sort.Strings(tests)

	links := m.FileLinks()
	imports := m.ImportTargets()

	// Dependency edges between non-test files. Tests are not depended on,
	// so they never appear as the source of an edge; the test map covers
	// them.
	for _, f := range m.Files {
		if !m.IsCodeFile(f.Path) || IsTestFile(f.Path) {
			continue
		}
		set := map[string]bool{}
		for _, t := range links[f.Path] {
			set[t] = true
		}
		for _, t := range imports[f.Path] {
			set[t] = true
		}
		var targets []string
		for t := range set {
			if t != f.Path && m.IsCodeFile(t) && !IsTestFile(t) {
				targets = append(targets, t)
			}
		}
		if len(targets) > 0 {
			sort.Strings(targets)
			d.edges[f.Path] = targets
		}
	}

	d.modules = buildModules(sources)
	moduleAt := map[string]int{}
	for i, mod := range d.modules {
		moduleAt[mod.Path] = i
	}
	moduleOf := func(file string) int {
		dir := slashDir(file)
		for {
			if i, ok := moduleAt[dir]; ok {
				return i
			}
			if dir == "." || dir == "" || dir == "/" {
				return -1
			}
			dir = slashDir(dir)
		}
	}

	// Module dependencies from file edges.
	deps := make([]map[string]bool, len(d.modules))
	users := make([]map[string]bool, len(d.modules))
	for i := range d.modules {
		deps[i], users[i] = map[string]bool{}, map[string]bool{}
	}
	for from, tos := range d.edges {
		fi := moduleOf(from)
		if fi < 0 {
			continue
		}
		for _, to := range tos {
			ti := moduleOf(to)
			if ti < 0 || ti == fi {
				continue
			}
			deps[fi][d.modules[ti].Path] = true
			users[ti][d.modules[fi].Path] = true
		}
	}
	for i := range d.modules {
		d.modules[i].DependsOn = sortedKeys(deps[i])
		d.modules[i].UsedBy = sortedKeys(users[i])
	}
	for _, t := range tests {
		if i := moduleOf(t); i >= 0 {
			d.modules[i].TestFiles++
		}
	}

	d.entries = findEntries(m, root, sources)
	for _, e := range d.entries {
		if i := moduleOf(e.Path); i >= 0 {
			d.modules[i].Entry = true
		}
	}

	d.testMap = buildTestMap(sources, tests, links, imports)
	return d
}

// buildModules groups source files by directory.
func buildModules(sources map[string]*codemap.File) []Module {
	type acc struct {
		files   []*codemap.File
		langs   map[string]int
		lines   int
		exports []string
	}
	byDir := map[string]*acc{}
	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		f := sources[p]
		dir := slashDir(p)
		a := byDir[dir]
		if a == nil {
			a = &acc{langs: map[string]int{}}
			byDir[dir] = a
		}
		a.files = append(a.files, f)
		a.langs[f.Language]++
		a.lines += f.Lines
		for _, s := range f.Symbols {
			if s.Exported && s.Container == "" && s.Kind != "method" && len(a.exports) < maxExports*3 {
				a.exports = append(a.exports, s.Name)
			}
		}
	}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	mods := make([]Module, 0, len(dirs))
	for _, dir := range dirs {
		a := byDir[dir]
		mods = append(mods, Module{
			Path:     dir,
			Language: topKey(a.langs),
			Files:    len(a.files),
			Lines:    a.lines,
			Exports:  uniqueFirst(a.exports, maxExports),
		})
	}
	return mods
}

// findEntries lists where the program starts.
func findEntries(m *codemap.Map, root string, sources map[string]*codemap.File) []Entry {
	seen := map[string]bool{}
	var out []Entry
	add := func(p, kind string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, Entry{Path: p, Kind: kind})
		}
	}
	for _, p := range m.EntryFiles() {
		kind := "named"
		if f := sources[p]; f != nil {
			for _, s := range f.Symbols {
				if s.Name == "main" && s.Kind == "function" {
					kind = "main"
					break
				}
			}
		}
		add(p, kind)
	}
	// Every Go main package is an entry even when the code map ranks only
	// the most significant few.
	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		f := sources[p]
		if f.Language != "go" || path.Base(p) != "main.go" {
			continue
		}
		for _, s := range f.Symbols {
			if s.Name == "main" && s.Kind == "function" {
				add(p, "main")
				break
			}
		}
	}
	for _, p := range manifestEntries(root, sources) {
		add(p, "script")
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}

// manifestEntries reads the files a package.json names as the package's
// main file or binaries.
func manifestEntries(root string, sources map[string]*codemap.File) []string {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil
	}
	var manifest struct {
		Main string          `json:"main"`
		Bin  json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return nil
	}
	candidates := []string{manifest.Main}
	var single string
	var multi map[string]string
	if json.Unmarshal(manifest.Bin, &single) == nil {
		candidates = append(candidates, single)
	} else if json.Unmarshal(manifest.Bin, &multi) == nil {
		keys := sortedKeysOf(multi)
		for _, k := range keys {
			candidates = append(candidates, multi[k])
		}
	}
	var out []string
	for _, c := range candidates {
		c = path.Clean(strings.TrimPrefix(filepath.ToSlash(c), "./"))
		if c == "." || c == "" {
			continue
		}
		if _, ok := sources[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

// buildTestMap relates each source file to the tests that cover it.
func buildTestMap(sources map[string]*codemap.File, tests []string, links, imports map[string][]string) map[string][]TestRef {
	byStem := map[string][]string{}
	byDir := map[string][]string{}
	for p := range sources {
		stem := strings.ToLower(strings.TrimSuffix(path.Base(p), path.Ext(p)))
		byStem[stem] = append(byStem[stem], p)
		byDir[slashDir(p)] = append(byDir[slashDir(p)], p)
	}
	covers := map[string]map[string]string{} // Source to test to kind.
	note := func(source, test, kind string) {
		if covers[source] == nil {
			covers[source] = map[string]string{}
		}
		if old, ok := covers[source][test]; !ok || kindRank(kind) < kindRank(old) {
			covers[source][test] = kind
		}
	}
	for _, t := range tests {
		dir := slashDir(t)
		if stem := testStem(t); stem != "" {
			matches := byStem[stem]
			var local []string
			for _, s := range matches {
				if slashDir(s) == dir {
					local = append(local, s)
				}
			}
			switch {
			case len(local) > 0:
				for _, s := range local {
					note(s, t, TestDirect)
				}
			case len(matches) > 0 && len(matches) <= maxStemMatches:
				for _, s := range matches {
					note(s, t, TestDirect)
				}
			}
		}
		if strings.HasSuffix(strings.ToLower(t), "_test.go") {
			for _, s := range byDir[dir] {
				if strings.HasSuffix(s, ".go") {
					note(s, t, TestPackage)
				}
			}
		}
		targets := links[t]
		if len(targets) == 0 && len(imports[t]) <= maxImportFallback {
			targets = imports[t]
		}
		for _, s := range targets {
			if _, ok := sources[s]; ok {
				note(s, t, TestImports)
			}
		}
	}
	out := make(map[string][]TestRef, len(covers))
	for source, set := range covers {
		refs := make([]TestRef, 0, len(set))
		for test, kind := range set {
			refs = append(refs, TestRef{Path: test, Kind: kind})
		}
		sort.Slice(refs, func(i, j int) bool {
			if kindRank(refs[i].Kind) != kindRank(refs[j].Kind) {
				return kindRank(refs[i].Kind) < kindRank(refs[j].Kind)
			}
			return refs[i].Path < refs[j].Path
		})
		if len(refs) > maxTestsPerSource {
			refs = refs[:maxTestsPerSource]
		}
		out[source] = refs
	}
	return out
}

func kindRank(kind string) int {
	switch kind {
	case TestDirect:
		return 0
	case TestPackage:
		return 1
	}
	return 2
}

// detectTestCommands names the commands that run the project's tests, by
// the manifests present at the root.
func detectTestCommands(root string, hasPythonTests bool) []string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	var out []string
	if exists("go.mod") {
		out = append(out, "go test ./...")
	}
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var manifest struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(data, &manifest) == nil && manifest.Scripts["test"] != "" {
			out = append(out, "npm test")
		}
	}
	if exists("pytest.ini") || exists("tox.ini") || exists("pyproject.toml") && hasPythonTests || hasPythonTests && exists("setup.py") {
		out = append(out, "pytest")
	}
	if exists("Cargo.toml") {
		out = append(out, "cargo test")
	}
	if exists("pom.xml") {
		out = append(out, "mvn test")
	}
	if exists("build.gradle") || exists("build.gradle.kts") {
		out = append(out, "gradle test")
	}
	if exists("mix.exs") {
		out = append(out, "mix test")
	}
	return out
}

func topKey(counts map[string]int) string {
	best, n := "", 0
	for k, v := range counts {
		if v > n || v == n && k < best {
			best, n = k, v
		}
	}
	return best
}

func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// uniqueFirst returns the first n distinct values.
func uniqueFirst(values []string, n int) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
			if len(out) == n {
				break
			}
		}
	}
	return out
}
