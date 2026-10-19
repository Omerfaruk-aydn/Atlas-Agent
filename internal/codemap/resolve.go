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

