package projectmodel

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Limits on an impact answer.
const (
	DefaultImpactDepth = 3
	MaxImpactDepth     = 6
	maxImpactFiles     = 400
	maxImpactTests     = 30
	maxImpactCommands  = 8
	maxExpandedInputs  = 200
)

// Impact is what a change to some files reaches.
type Impact struct {
	// Changed are the inputs the model knows, directories expanded.
	Changed []string `json:"changed"`
	// Unknown are inputs the model has never seen: new files, typos,
	// ignored paths.
	Unknown []string `json:"unknown,omitempty"`
	// Direct are the files that use a changed file; Indirect reach it
	// through others, up to the depth asked for.
	Direct   []string `json:"direct,omitempty"`
	Indirect []string `json:"indirect,omitempty"`
	Modules  []string `json:"modules,omitempty"`
	// Tests are the tests worth running, nearest first.
	Tests    []TestRef `json:"tests,omitempty"`
	Commands []string  `json:"commands,omitempty"`
	// Owners lists, per changed module, who to ask.
	Owners    []ModuleOwners `json:"owners,omitempty"`
	Decisions []Decision     `json:"decisions,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
}

// ModuleOwners is a module and its main committers.
type ModuleOwners struct {
	Module string  `json:"module"`
	Owners []Owner `json:"owners"`
}

// NormalizePath turns an input path into the form the model stores.
func NormalizePath(p string) string {
	p = strings.TrimSpace(filepath.ToSlash(p))
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// Impact reports what changing files reaches: the files that depend on
// them, the modules involved, the tests that cover them and the people and
// decisions concerned. A directory stands for every file under it.
func (m *Model) Impact(files []string, depth int) Impact {
	if depth <= 0 {
		depth = DefaultImpactDepth
	}
	depth = min(depth, MaxImpactDepth)
	ix := m.ensureIndex()
	var out Impact

	changed := map[string]bool{}
	for _, in := range files {
		p := NormalizePath(in)
		if p == "" {
			continue
		}
		if _, ok := m.Files[p]; ok {
			changed[p] = true
			continue
		}
		prefix := p + "/"
		found := false
		for f := range m.Files {
			if strings.HasPrefix(f, prefix) && len(changed) < maxExpandedInputs {
				changed[f] = true
				found = true
			}
		}
		if !found {
			out.Unknown = append(out.Unknown, p)
		}
	}
	out.Changed = sortedKeys(changed)
	sort.Strings(out.Unknown)

	// Walk dependents breadth first.
	distance := map[string]int{}
	for f := range changed {
		distance[f] = 0
	}
	frontier := out.Changed
	for level := 1; level <= depth && len(frontier) > 0; level++ {
		var next []string
		for _, f := range frontier {
			for _, user := range ix.usedBy[f] {
				if _, seen := distance[user]; seen {
					continue
				}
				if len(distance)-len(changed) >= maxImpactFiles {
					out.Truncated = true
					continue
				}
				distance[user] = level
				next = append(next, user)
				if level == 1 {
					out.Direct = append(out.Direct, user)
				} else {
					out.Indirect = append(out.Indirect, user)
				}
			}
		}
		sort.Strings(next)
		frontier = next
	}
	sort.Strings(out.Direct)
	sort.Strings(out.Indirect)

	// Modules and owners.
	modules := map[string]bool{}
	changedModules := map[string]bool{}
	for f, dist := range distance {
		if mod := m.ModuleOf(f); mod != nil {
			modules[mod.Path] = true
			if dist == 0 {
				changedModules[mod.Path] = true
			}
		}
	}
	out.Modules = sortedKeys(modules)
	for _, p := range sortedKeys(changedModules) {
		if mod := m.ModuleByPath(p); mod != nil && len(mod.Owners) > 0 {
			out.Owners = append(out.Owners, ModuleOwners{Module: p, Owners: mod.Owners})
		}
	}

	out.Tests = m.testsFor(distance, changed)
	out.Commands = m.commandsFor(out.Tests)
	out.Decisions = m.relevantDecisions(out.Changed, sortedKeys(changedModules))
	return out
}

// testsFor ranks the tests covering the given files, nearest first. A
// changed test file is itself the first thing to run.
func (m *Model) testsFor(distance map[string]int, changed map[string]bool) []TestRef {
	type scored struct {
		ref   TestRef
		score int
	}
	best := map[string]scored{}
	offer := func(ref TestRef, dist int) {
		score := dist*10 + kindRank(ref.Kind)
		if old, ok := best[ref.Path]; !ok || score < old.score {
			best[ref.Path] = scored{ref, score}
		}
	}
	for f := range changed {
		if IsTestFile(f) {
			offer(TestRef{Path: f, Kind: "changed"}, -1)
		}
	}
	for f, dist := range distance {
		for _, ref := range m.TestMap[f] {
			offer(ref, dist)
		}
	}
	list := make([]scored, 0, len(best))
	for _, s := range best {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score < list[j].score
		}
		return list[i].ref.Path < list[j].ref.Path
	})
	if len(list) > maxImpactTests {
		list = list[:maxImpactTests]
	}
	out := make([]TestRef, len(list))
	for i, s := range list {
		out[i] = s.ref
	}
	return out
}

// commandsFor suggests commands that run the given tests.
func (m *Model) commandsFor(tests []TestRef) []string {
	var cmds []string
	seen := map[string]bool{}
	add := func(c string) {
		if !seen[c] && len(cmds) < maxImpactCommands {
			seen[c] = true
			cmds = append(cmds, c)
		}
	}
	for _, t := range tests {
		switch {
		case strings.HasSuffix(t.Path, "_test.go"):
			dir := path.Dir(t.Path)
			if dir == "." {
				add("go test .")
			} else {
				add("go test ./" + dir)
			}
		case strings.HasSuffix(t.Path, ".py"):
			add("pytest " + t.Path)
		}
	}
	if len(tests) == 0 && len(m.TestCommands) > 0 {
		add(m.TestCommands[0])
	}
	return cmds
}

// relevantDecisions returns the decisions that concern the files or modules.
func (m *Model) relevantDecisions(files, modules []string) []Decision {
	var out []Decision
	for _, d := range m.Decisions {
		if decisionConcerns(d, files, modules) {
			out = append(out, d)
		}
	}
	return out
}

func decisionConcerns(d Decision, files, modules []string) bool {
	for _, scope := range d.Files {
		scope = NormalizePath(scope)
		if scope == "" {
			continue
		}
		for _, f := range files {
			if f == scope || strings.HasPrefix(f, scope+"/") {
				return true
			}
		}
		for _, mod := range modules {
			if mod == scope || strings.HasPrefix(mod, scope+"/") || strings.HasPrefix(scope, mod+"/") {
				return true
			}
		}
	}
	if d.Source != "adr" && len(d.Files) > 0 {
		return false
	}
	text := strings.ToLower(d.Text + " " + d.Rationale)
	for _, f := range files {
		if strings.Contains(text, strings.ToLower(f)) {
			return true
		}
	}
	for _, mod := range modules {
		if mod != "." && (strings.Contains(text, strings.ToLower(mod)+"/") || strings.Contains(text, strings.ToLower(mod)+" ")) {
			return true
		}
		if d.Source == "adr" {
			if name := strings.ToLower(path.Base(mod)); len(name) >= 4 && strings.Contains(text, name) {
				return true
			}
		}
	}
	return false
}
