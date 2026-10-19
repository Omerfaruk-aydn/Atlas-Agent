package codemap

import (
	"path"
	"slices"
	"sort"
	"strings"
)

// maxCandidates bounds how many definitions an ambiguous use links to; a
// name defined more often than this (get, update, render) says nothing
// about which one is meant.
const maxCandidates = 6

// Map is the resolved map of a project.
type Map struct {
	Root      string
	Files     []*File
	Truncated bool
	// Skipped lists files left out and why.
	Skipped []string

	byPath  map[string]int
	symbols []symbolRef
	// firstSymbol is the global index of each file's first symbol.
	firstSymbol []int
	// nested marks definitions inside a function: locals, callbacks.
	nested []bool
	byName map[string][]int
	// byQualified indexes Container.Name.
	byQualified map[string][]int
	// namespaces maps a namespace or module name to the files that
	// declare it (C#, C++, Elixir).
	namespaces map[string][]int
	// suffixes maps a path without extension, and each of its trailing
	// segments, to files.
	suffixes map[string][]int
	dirs     map[string][]int

	// imports holds the resolved target files of each import of each
	// file, aligned with File.Imports; nil targets are external.
	imports [][][]int
	links   []Link
	// in and out index links by symbol.
	in, out map[int][]int
	// fileLinks are weighted file-to-file dependencies.
	fileLinks map[[2]int]float64

	fileRank    []float64
	fileScore   []float64
	symbolScore []float64
	community   []int
	communities []community
	history     *project

	// code marks the files that take part in the dependency graph:
	// everything but documents and assets.
	code []bool
	// symDomain is each symbol's domain; byDomain indexes the definitions
	// outside code by domain and key.
	symDomain []string
	byDomain  map[string]map[string][]int
	// pathSuffix maps each path and each of its trailing segments, with
	// the extension, to files.
	pathSuffix map[string][]int
	// importedBy lists the files that import each file.
	importedBy map[int][]int
	// paths holds what each FileLink of each file resolved to, aligned
	// with File.Links; pathIn and anchorIn index them by target file and
	// by target section.
	paths    [][]pathTarget
	pathIn   map[int][]pathUse
	anchorIn map[int][]pathUse
	// others are the project's files the map does not read, known by
	// name so a link to one is not reported missing; folders are every
	// directory holding a file.
	others  map[string]bool
	folders map[string]bool
}

// symbolRef locates a symbol: Files[file].Symbols[index].
type symbolRef struct{ file, index int }

// Link is a use of a definition.
type Link struct {
	// From is the symbol the use is in, or -1-file for a use at the top
	// level of a file.
	From       int
	To         int
	Kind       string
	Confidence Confidence
	// Candidates is how many definitions an ambiguous use matched.
	Candidates int
	File, Line int
}

func (m *Map) symbol(i int) *Symbol {
	r := m.symbols[i]
	return &m.Files[r.file].Symbols[r.index]
}

func (m *Map) fileOf(i int) int {
	if i < 0 {
		return -1 - i
	}
	return m.symbols[i].file
}

// newMap indexes files and resolves every import and use. others are the
// paths of the project's files the map does not read.
func newMap(root string, files []*File, module string, others []string) *Map {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	m := &Map{
		Root: root, Files: files, byPath: map[string]int{}, byName: map[string][]int{},
		byQualified: map[string][]int{}, namespaces: map[string][]int{}, suffixes: map[string][]int{},
		dirs: map[string][]int{}, in: map[int][]int{}, out: map[int][]int{}, fileLinks: map[[2]int]float64{},
		byDomain: map[string]map[string][]int{}, pathSuffix: map[string][]int{}, importedBy: map[int][]int{},
		others: make(map[string]bool, len(others)), folders: map[string]bool{},
	}
	addFolders := func(p string) {
		for d := path.Dir(p); d != "." && d != "/" && !m.folders[d]; d = path.Dir(d) {
			m.folders[d] = true
		}
	}
	for _, p := range others {
		m.others[p] = true
		addFolders(p)
	}
	for _, f := range files {
		addFolders(f.Path)
	}
	m.firstSymbol = make([]int, len(files))
	m.code = make([]bool, len(files))
	for fi, f := range files {
		m.firstSymbol[fi] = len(m.symbols)
		m.byPath[f.Path] = fi
		m.code[fi] = Category(f.Language) == CategoryCode
		dir := path.Dir(f.Path)
		m.dirs[dir] = append(m.dirs[dir], fi)
		parts := strings.Split(f.Path, "/")
		for k := range parts {
			key := strings.Join(parts[k:], "/")
			m.pathSuffix[key] = append(m.pathSuffix[key], fi)
		}
		if m.code[fi] {
			parts := strings.Split(strings.TrimSuffix(f.Path, path.Ext(f.Path)), "/")
			for k := range parts {
				key := strings.Join(parts[k:], "/")
				m.suffixes[key] = append(m.suffixes[key], fi)
			}
		}
		folds := languages[f.Language] != nil && languages[f.Language].caseInsensitive
		for si := range f.Symbols {
			s := &f.Symbols[si]
			id := len(m.symbols)
			m.symbols = append(m.symbols, symbolRef{fi, si})
			domain := symbolDomain(f.Language, s.Kind)
			m.symDomain = append(m.symDomain, domain)
			if domain != domainCode {
				idx := m.byDomain[domain]
				if idx == nil {
					idx = map[string][]int{}
					m.byDomain[domain] = idx
				}
				key := domainKey(domain, s)
				idx[key] = append(idx[key], id)
				if s.Container != "" && domain != domainTask {
					idx[s.Container+"."+key] = append(idx[s.Container+"."+key], id)
				}
				continue
			}
			names := []string{s.Name}
			if folds && strings.ToLower(s.Name) != s.Name {
				// Uses in these languages are written in lower case.
				names = append(names, strings.ToLower(s.Name))
			}
			for _, name := range names {
				m.byName[name] = append(m.byName[name], id)
				if s.Container != "" {
					container := s.Container
					if folds {
						container = strings.ToLower(container)
					}
					m.byQualified[container+"."+name] = append(m.byQualified[container+"."+name], id)
				}
				if s.Kind == "namespace" {
					m.namespaces[name] = appendUnique(m.namespaces[name], fi)
				}
			}
		}
	}
	m.markNested()
	m.imports = make([][][]int, len(files))
	for fi, f := range files {
		m.imports[fi] = make([][]int, len(f.Imports))
		for k, imp := range f.Imports {
			m.imports[fi][k] = m.resolveImport(fi, imp, module)
			for _, t := range m.imports[fi][k] {
				if t != fi {
					m.importedBy[t] = appendUnique(m.importedBy[t], fi)
				}
			}
			// An import of one file is a dependency on it. An import of a
			// package or namespace spanning several files is not one on
			// each of them; the uses of its definitions say which.
			if targets := m.imports[fi][k]; len(targets) == 1 && targets[0] != fi && m.code[fi] {
				m.fileLinks[[2]int{fi, targets[0]}]++
			}
		}
	}
	m.resolvePaths()
	for fi := range files {
		m.resolveRefs(fi)
	}
	m.rank()
	m.cluster()
	return m
}

// markNested marks the definitions that lie inside a function or method.
func (m *Map) markNested() {
	m.nested = make([]bool, len(m.symbols))
	for fi, f := range m.Files {
		order := make([]int, len(f.Symbols))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool {
			sa, sb := f.Symbols[order[a]], f.Symbols[order[b]]
			if sa.Line != sb.Line {
				return sa.Line < sb.Line
			}
			return sa.EndLine > sb.EndLine
		})
		var open []int
		for _, i := range order {
			s := f.Symbols[i]
			for len(open) > 0 && f.Symbols[open[len(open)-1]].EndLine < s.Line {
				open = open[:len(open)-1]
			}
			for _, o := range open {
				if k := f.Symbols[o].Kind; (k == "function" || k == "method") && f.Symbols[o].EndLine > f.Symbols[o].Line {
					m.nested[m.firstSymbol[fi]+i] = true
					break
				}
			}
			open = append(open, i)
		}
	}
}

func appendUnique(list []int, v int) []int {
	if slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}

// sourceExts are the extensions an import of each language can resolve
// to.
var sourceExts = map[string][]string{
	"typescript": {".ts", ".tsx", ".d.ts", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts", ".vue", ".svelte"},
	"javascript": {".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".vue", ".svelte"},
	"python":     {".py", ".pyi"},
	"java":       {".java", ".kt"},
	"kotlin":     {".kt", ".java"},
	"scala":      {".scala"},
	"groovy":     {".groovy", ".java"},
	"php":        {".php"},
	"ruby":       {".rb"},
	"lua":        {".lua"},
	"perl":       {".pm", ".pl"},
	"dart":       {".dart"},
	"rust":       {".rs"},
	"mojo":       {".mojo", ".py"},
	"pyproject":  {".py"},
	"crystal":    {".cr"},
	"haskell":    {".hs", ".lhs"},
	"elm":        {".elm"},
	"ocaml":      {".ml", ".mli"},
	"clojure":    {".clj", ".cljs", ".cljc"},
	"lisp":       {".el", ".lisp", ".lsp", ".scm", ".ss", ".rkt"},
	"erlang":     {".erl", ".hrl"},
	"gleam":      {".gleam"},
	"nim":        {".nim"},
	"solidity":   {".sol"},
	"powershell": {".ps1", ".psm1"},
	"pascal":     {".pas", ".pp"},
	"gdscript":   {".gd"},
	"godot":      {".gd", ".cs"},
	"scss":       {".scss", ".sass", ".css"},
	"less":       {".less", ".css"},
	"css":        {".css"},
}

// lookup returns the files at p, trying each extension and index file.
func (m *Map) lookup(p string, exts []string, indexes ...string) []int {
	p = path.Clean(p)
	if fi, ok := m.byPath[p]; ok {
		return []int{fi}
	}
	for _, ext := range exts {
		if fi, ok := m.byPath[p+ext]; ok {
			return []int{fi}
		}
	}
	for _, index := range indexes {
		for _, ext := range exts {
			if fi, ok := m.byPath[p+"/"+index+ext]; ok {
				return []int{fi}
			}
		}
	}
	return nil
}

// bySuffix returns the files whose path, without extension, ends in rel
// and whose extension is one of exts; only a unique match counts.
func (m *Map) bySuffix(rel string, exts []string) []int {
	var out []int
	for _, fi := range m.suffixes[strings.Trim(rel, "/")] {
		if slices.Contains(exts, path.Ext(m.Files[fi].Path)) {
			out = append(out, fi)
		}
	}
	if len(out) == 1 {
		return out
	}
	return nil
}

