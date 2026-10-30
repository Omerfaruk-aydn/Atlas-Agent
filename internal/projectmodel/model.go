// Package projectmodel keeps one incrementally refreshed model of a
// project: its modules and how they depend on each other, where the program
// starts, which tests cover which files, who owns what according to the
// version-control history, the documents that explain it and the decisions
// recorded about it.
//
// It does not parse anything itself. The code graph comes from codemap, the
// decisions from factstore, the documents from docindex and the ownership
// from the commit history. This package joins them into a single document
// stored under the project's data directory, and answers the questions the
// joined view makes cheap: what a change reaches, which tests to run, who to
// ask.
package projectmodel

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Version changes when the stored shape or the meaning of a field changes,
// so an older file is rebuilt instead of trusted.
const Version = 1

// FileName is the model's file name inside the data directory.
const FileName = "project_model.json"

// Model is the stored project model. Everything in it derives from the
// project's files, its history and the recorded decisions; nothing in it
// depends on who reads it or when, so two builds of the same project state
// are identical apart from BuiltAt.
type Model struct {
	Version int `json:"version"`
	// BuiltAt is when the model last changed.
	BuiltAt string `json:"built_at"`
	// Head is the commit the ownership was read at; empty without history.
	Head string `json:"head,omitempty"`
	// Fingerprint identifies the inputs the model was built from.
	Fingerprint string `json:"fingerprint"`
	GoModule    string `json:"go_module,omitempty"`

	// Files maps each code and document file to a short content hash. It
	// is what an incremental refresh compares against.
	Files map[string]string `json:"files"`
	// Languages counts source files by language.
	Languages    map[string]int `json:"languages,omitempty"`
	TestCommands []string       `json:"test_commands,omitempty"`

	Modules []Module `json:"modules"`
	Entries []Entry  `json:"entries,omitempty"`
	// Edges maps a file to the files it depends on.
	Edges map[string][]string `json:"edges,omitempty"`
	// TestMap maps a source file to the tests that cover it.
	TestMap   map[string][]TestRef `json:"test_map,omitempty"`
	Docs      []Doc                `json:"docs,omitempty"`
	Decisions []Decision           `json:"decisions,omitempty"`

	index *index
}

// Module is a directory of source files that are maintained together: a Go
// package, a JavaScript folder, a Python package.
type Module struct {
	Path      string   `json:"path"`
	Language  string   `json:"language"`
	Files     int      `json:"files"`
	TestFiles int      `json:"test_files,omitempty"`
	Lines     int      `json:"lines"`
	Exports   []string `json:"exports,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	UsedBy    []string `json:"used_by,omitempty"`
	Entry     bool     `json:"entry,omitempty"`
	Owners    []Owner  `json:"owners,omitempty"`
}

// Owner is an author's share of the commits that touched a module.
type Owner struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
	// Share is the percentage of the module's commits.
	Share int `json:"share"`
}

// Entry is a place where the program starts.
type Entry struct {
	Path string `json:"path"`
	// Kind is main (defines a main function), script (named by the package
	// manifest) or named (called like an entry point and imported by
	// nothing).
	Kind string `json:"kind"`
}

// Test kinds, from the most to the least specific.
const (
	// TestDirect is a test named after the file it covers.
	TestDirect = "direct"
	// TestPackage is a test in the same package or directory.
	TestPackage = "package"
	// TestImports is a test that imports the file or reaches it through a
	// use of one of its definitions.
	TestImports = "imports"
)

// TestRef is one test file and why it counts as covering a source file.
type TestRef struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// Doc is a document worth knowing about.
type Doc struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	// ADR marks an architecture decision record.
	ADR bool `json:"adr,omitempty"`
}

// Decision is a recorded choice and, when given, the reason for it.
type Decision struct {
	ID        string   `json:"id,omitempty"`
	Text      string   `json:"text"`
	Rationale string   `json:"rationale,omitempty"`
	Files     []string `json:"files,omitempty"`
	// Source is fact for a decision recorded with the project_model tool or
	// the facts tool, and adr for a decision record document.
	Source string `json:"source"`
	// Path is the document of an adr decision.
	Path string `json:"path,omitempty"`
}

// locks serializes writers of one model file within this process; the key is
// the model path.
var locks sync.Map

// lockFor returns the mutex of one model file. Writers in other processes
// are covered by the atomic rename in Save.
func lockFor(path string) *sync.Mutex {
	l, _ := locks.LoadOrStore(path, &sync.Mutex{})
	return l.(*sync.Mutex)
}

// Path returns where the model of a project is stored.
func Path(dataDir string) string { return filepath.Join(dataDir, FileName) }

// Load reads the stored model. It returns nil without an error when there
// is none or it was written by another version.
func Load(dataDir string) (*Model, error) {
	data, err := os.ReadFile(Path(dataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Model
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("project model is corrupt, rebuild it: %w", err)
	}
	if m.Version != Version {
		return nil, nil
	}
	return &m, nil
}

// Save writes the model atomically.
func Save(dataDir string, m *Model) error {
	path := Path(dataDir)
	mu := lockFor(path)
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dataDir, FileName+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// index holds lookups derived from the stored fields.
type index struct {
	module map[string]int
	usedBy map[string][]string
}

// ensureIndex builds the lookups once.
func (m *Model) ensureIndex() *index {
	if m.index != nil {
		return m.index
	}
	ix := &index{
		module: make(map[string]int, len(m.Modules)),
		usedBy: map[string][]string{},
	}
	for i, mod := range m.Modules {
		ix.module[mod.Path] = i
	}
	for from, tos := range m.Edges {
		for _, to := range tos {
			ix.usedBy[to] = append(ix.usedBy[to], from)
		}
	}
	for k := range ix.usedBy {
		sort.Strings(ix.usedBy[k])
	}
	m.index = ix
	return ix
}

// ModuleOf returns the module a file belongs to: the module with the
// longest path that contains it, or nil.
func (m *Model) ModuleOf(file string) *Module {
	ix := m.ensureIndex()
	dir := slashDir(file)
	for {
		if i, ok := ix.module[dir]; ok {
			return &m.Modules[i]
		}
		if dir == "." || dir == "" {
			return nil
		}
		dir = slashDir(dir)
	}
}

// ModuleByPath returns the module at exactly this path, or nil.
func (m *Model) ModuleByPath(path string) *Module {
	if i, ok := m.ensureIndex().module[path]; ok {
		return &m.Modules[i]
	}
	return nil
}

// UsedBy returns the files that depend on file.
func (m *Model) UsedBy(file string) []string { return m.ensureIndex().usedBy[file] }
