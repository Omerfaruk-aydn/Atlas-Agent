package codemap

import "sort"

// FileLinks returns, for every code file, the files it depends on through
// resolved uses of their definitions or single-file imports. The slices are
// sorted; files without dependencies are absent.
func (m *Map) FileLinks() map[string][]string {
	out := map[string][]string{}
	for key := range m.fileLinks {
		from, to := m.Files[key[0]].Path, m.Files[key[1]].Path
		out[from] = append(out[from], to)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// ImportTargets returns, for every file, the project files its imports
// resolve to. An import of a package spanning several files lists all of
// them. External imports resolve to nothing and are absent. The slices are
// sorted and free of duplicates.
func (m *Map) ImportTargets() map[string][]string {
	out := map[string][]string{}
	for fi, perImport := range m.imports {
		seen := map[int]bool{}
		var targets []string
		for _, ts := range perImport {
			for _, t := range ts {
				if t != fi && !seen[t] {
					seen[t] = true
					targets = append(targets, m.Files[t].Path)
				}
			}
		}
		if len(targets) > 0 {
			sort.Strings(targets)
			out[m.Files[fi].Path] = targets
		}
	}
	return out
}

// EntryFiles returns the files where the program starts, most significant
// first.
func (m *Map) EntryFiles() []string { return m.entryPoints() }

// IsCodeFile reports whether path takes part in the dependency graph:
// everything but documents and assets.
func (m *Map) IsCodeFile(path string) bool {
	fi, ok := m.byPath[path]
	return ok && m.code[fi]
}

// ModulePath returns the module path read from root's go.mod, or "".
func ModulePath(root string) string { return goModule(root) }
