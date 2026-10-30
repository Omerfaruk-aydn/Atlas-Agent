package projectmodel

import (
	"fmt"
	"sort"
	"strings"
)

const (
	listLimit     = 12
	matchLimit    = 10
	defaultBudget = 3000
	maxBudget     = 12000
)

// list renders up to limit items and says how many were left out.
func list(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:limit], ", ") + fmt.Sprintf(", and %d more", len(items)-limit)
}

func formatOwners(owners []Owner) string {
	parts := make([]string, len(owners))
	for i, o := range owners {
		parts[i] = fmt.Sprintf("%s %d%%", o.Name, o.Share)
	}
	return strings.Join(parts, ", ")
}

func formatTests(refs []TestRef, limit int) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.Path + " (" + r.Kind + ")"
	}
	return list(parts, limit)
}

func formatDecision(d Decision) string {
	s := d.Text
	if d.Rationale != "" {
		s += " (why: " + d.Rationale + ")"
	}
	if d.Source == "adr" {
		s += " [" + d.Path + "]"
	} else if len(d.Files) > 0 {
		s += " [" + strings.Join(d.Files, ", ") + "]"
	}
	return s
}

// Describe answers what the model knows about a module or file: its
// neighbours, tests and owners. A name that is neither lists the modules and
// files it could mean.
func (m *Model) Describe(target string) string {
	target = NormalizePath(target)
	if target == "" {
		return "Name a module (a directory), a file, or part of a path."
	}
	if mod := m.ModuleByPath(target); mod != nil {
		return m.describeModule(mod)
	}
	if _, ok := m.Files[target]; ok {
		return m.describeFile(target)
	}
	needle := strings.ToLower(target)
	var mods, files []string
	for _, mod := range m.Modules {
		if strings.Contains(strings.ToLower(mod.Path), needle) {
			mods = append(mods, mod.Path)
		}
	}
	for f := range m.Files {
		if strings.Contains(strings.ToLower(f), needle) {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	if len(mods) == 1 && len(files) <= 1 || len(mods) == 0 && len(files) == 1 {
		if len(mods) == 1 {
			return m.describeModule(m.ModuleByPath(mods[0]))
		}
		return m.describeFile(files[0])
	}
	if len(mods) == 0 && len(files) == 0 {
		return fmt.Sprintf("Nothing in the project model matches %q.", target)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Several things match %q; ask again with one of them.\n", target)
	if len(mods) > 0 {
		b.WriteString("Modules: " + list(mods, matchLimit) + "\n")
	}
	if len(files) > 0 {
		b.WriteString("Files: " + list(files, matchLimit) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) describeModule(mod *Module) string {
	var b strings.Builder
	kind := mod.Language
	if mod.Entry {
		kind += ", entry point"
	}
	fmt.Fprintf(&b, "Module %s (%s): %d files, %d lines, %d test files.\n", mod.Path, kind, mod.Files, mod.Lines, mod.TestFiles)
	if len(mod.Exports) > 0 {
		b.WriteString("Defines: " + strings.Join(mod.Exports, ", ") + "\n")
	}
	if len(mod.DependsOn) > 0 {
		b.WriteString("Depends on: " + list(mod.DependsOn, listLimit) + "\n")
	}
	if len(mod.UsedBy) > 0 {
		b.WriteString("Used by: " + list(mod.UsedBy, listLimit) + "\n")
	}
	if len(mod.Owners) > 0 {
		b.WriteString("Owners: " + formatOwners(mod.Owners) + "\n")
	}
	if ds := m.relevantDecisions(nil, []string{mod.Path}); len(ds) > 0 {
		b.WriteString("Decisions:\n")
		for _, d := range ds[:min(len(ds), 5)] {
			b.WriteString("- " + formatDecision(d) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) describeFile(file string) string {
	var b strings.Builder
	mod := m.ModuleOf(file)
	if mod != nil {
		fmt.Fprintf(&b, "File %s, in module %s.\n", file, mod.Path)
	} else {
		fmt.Fprintf(&b, "File %s.\n", file)
	}
	if deps := m.Edges[file]; len(deps) > 0 {
		b.WriteString("Depends on: " + list(deps, listLimit) + "\n")
	}
	if users := m.UsedBy(file); len(users) > 0 {
		b.WriteString("Used by: " + list(users, listLimit) + "\n")
	}
	if tests := m.TestMap[file]; len(tests) > 0 {
		b.WriteString("Tests: " + formatTests(tests, listLimit) + "\n")
	} else if !IsTestFile(file) {
		b.WriteString("Tests: none found.\n")
	}
	if mod != nil && len(mod.Owners) > 0 {
		b.WriteString("Module owners: " + formatOwners(mod.Owners) + "\n")
	}
	var modules []string
	if mod != nil {
		modules = []string{mod.Path}
	}
	if ds := m.relevantDecisions([]string{file}, modules); len(ds) > 0 {
		b.WriteString("Decisions:\n")
		for _, d := range ds[:min(len(ds), 5)] {
			b.WriteString("- " + formatDecision(d) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatImpact renders an impact answer for a person or a model.
func FormatImpact(im Impact) string {
	var b strings.Builder
	if len(im.Changed) == 0 {
		b.WriteString("No known files to assess.\n")
	} else {
		fmt.Fprintf(&b, "Changing %s reaches %d files in %d modules.\n",
			list(im.Changed, 6), len(im.Direct)+len(im.Indirect), len(im.Modules))
	}
	if len(im.Unknown) > 0 {
		b.WriteString("Not in the model (new, ignored or misspelled): " + list(im.Unknown, listLimit) + "\n")
	}
	if len(im.Direct) > 0 {
		b.WriteString("Used directly by: " + list(im.Direct, listLimit) + "\n")
	}
	if len(im.Indirect) > 0 {
		b.WriteString("Reached indirectly: " + list(im.Indirect, listLimit) + "\n")
	}
	if len(im.Modules) > 0 {
		b.WriteString("Modules: " + list(im.Modules, listLimit) + "\n")
	}
	if len(im.Tests) > 0 {
		b.WriteString("Tests to run, nearest first: " + formatTests(im.Tests, 10) + "\n")
	} else if len(im.Changed) > 0 {
		b.WriteString("No test is mapped to these files.\n")
	}
	if len(im.Commands) > 0 {
		b.WriteString("Commands: " + strings.Join(im.Commands, "; ") + "\n")
	}
	for _, mo := range im.Owners {
		b.WriteString("Owners of " + mo.Module + ": " + formatOwners(mo.Owners) + "\n")
	}
	if len(im.Decisions) > 0 {
		b.WriteString("Decisions that apply:\n")
		for _, d := range im.Decisions[:min(len(im.Decisions), 6)] {
			b.WriteString("- " + formatDecision(d) + "\n")
		}
	}
	if im.Truncated {
		b.WriteString("The list of affected files was cut short.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// TestsOf lists the tests covering each file; a directory covers the files
// under it.
func (m *Model) TestsOf(files []string) string {
	var b strings.Builder
	for _, in := range files {
		p := NormalizePath(in)
		if p == "" {
			continue
		}
		if _, ok := m.Files[p]; ok {
			b.WriteString(p + ": ")
			if tests := m.TestMap[p]; len(tests) > 0 {
				b.WriteString(formatTests(tests, listLimit))
			} else if IsTestFile(p) {
				b.WriteString("is a test")
			} else {
				b.WriteString("no test found")
			}
			b.WriteString("\n")
			continue
		}
		im := m.Impact([]string{p}, 1)
		if len(im.Changed) == 0 {
			b.WriteString(p + ": not in the model\n")
			continue
		}
		b.WriteString(p + ": " + formatTests(im.Tests, listLimit) + "\n")
	}
	if b.Len() == 0 {
		return "Name at least one file or directory."
	}
	if len(m.TestCommands) > 0 {
		b.WriteString("Project test command: " + strings.Join(m.TestCommands, "; ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// OwnersOf lists the main committers of a module, or of the module holding
// a file. With no target it lists every module that has owners, largest
// first.
func (m *Model) OwnersOf(target string) string {
	target = NormalizePath(target)
	if target == "" {
		mods := append([]Module(nil), m.Modules...)
		sort.SliceStable(mods, func(i, j int) bool { return mods[i].Files > mods[j].Files })
		var b strings.Builder
		for _, mod := range mods {
			if len(mod.Owners) > 0 && b.Len() < 4000 {
				fmt.Fprintf(&b, "%s: %s\n", mod.Path, formatOwners(mod.Owners))
			}
		}
		if b.Len() == 0 {
			return "No ownership is known: the project has no commit history."
		}
		return strings.TrimRight(b.String(), "\n")
	}
	mod := m.ModuleByPath(target)
	if mod == nil {
		mod = m.ModuleOf(target)
	}
	if mod == nil {
		return fmt.Sprintf("%q is not inside a known module.", target)
	}
	if len(mod.Owners) == 0 {
		return fmt.Sprintf("No commit history is known for module %s.", mod.Path)
	}
	return fmt.Sprintf("Module %s is mostly maintained by %s.", mod.Path, formatOwners(mod.Owners))
}

// DecisionsAbout lists recorded decisions, filtered by a query matched
// against their text and scope. An empty query lists the latest.
func (m *Model) DecisionsAbout(query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	var hits []Decision
	for _, d := range m.Decisions {
		hay := strings.ToLower(formatDecision(d))
		if query == "" || strings.Contains(hay, query) {
			hits = append(hits, d)
		}
	}
	if len(hits) == 0 {
		if query == "" {
			return "No decisions have been recorded. Record one with action decide."
		}
		return fmt.Sprintf("No recorded decision mentions %q.", query)
	}
	if len(hits) > 20 {
		hits = hits[len(hits)-20:]
	}
	var b strings.Builder
	for _, d := range hits {
		b.WriteString("- " + formatDecision(d) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// Find lists modules and files whose path contains text.
func (m *Model) Find(text string) string {
	return m.Describe(text)
}

// Overview is the longer form of Summary: the same facts with more room.
func (m *Model) Overview(budget int) string {
	if budget <= 0 {
		budget = defaultBudget
	}
	budget = min(budget, maxBudget)
	if s := m.summaryWithin(budget, true); s != "" {
		return s
	}
	return m.summaryWithin(maxBudget, true)
}

// areaOf groups a module path into a coarse area of the project: two
// segments below a conventional source root, otherwise the first segment.
func areaOf(modulePath string) string {
	segs := strings.Split(modulePath, "/")
	if len(segs) == 1 {
		return segs[0]
	}
	switch segs[0] {
	case "internal", "pkg", "src", "packages", "apps", "lib", "libs", "services", "crates", "cmd", "app":
		return segs[0] + "/" + segs[1]
	}
	return segs[0]
}
