package projectmodel

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultSummaryBytes is the size the system prompt block may reach unless
// configured otherwise, and HardSummaryBytes the most it may ever be set to.
const (
	DefaultSummaryBytes = 2400
	HardSummaryBytes    = 8000
)

// shape says how much of each section a summary carries.
type shape struct {
	areas, core, entries, decisions, docs int
	owners, languages                     bool
}

// compactShapes run from the fullest to the barest; Summary picks the first
// that fits.
var compactShapes = []shape{
	{areas: 8, core: 6, entries: 5, decisions: 4, docs: 3, owners: true, languages: true},
	{areas: 6, core: 4, entries: 4, decisions: 3, owners: true, languages: true},
	{areas: 4, core: 3, entries: 3, decisions: 2, languages: true},
	{areas: 3, entries: 2},
}

var overviewShape = shape{areas: 25, core: 15, entries: 12, decisions: 12, docs: 10, owners: true, languages: true}

// Summary renders the compact block that goes into the system prompt. It is
// a pure function of the stored model, so it only changes when the project
// does, which keeps the prompt cache valid across turns. It returns "" when
// there is nothing to say or even the barest form exceeds maxBytes: a block
// that does not fit is worse than none.
func (m *Model) Summary(maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultSummaryBytes
	}
	maxBytes = min(maxBytes, HardSummaryBytes)
	if len(m.Modules) == 0 {
		return ""
	}
	for _, sh := range compactShapes {
		if s := m.render(sh); len(s) <= maxBytes {
			return s
		}
	}
	return ""
}

func (m *Model) summaryWithin(budget int, _ bool) string {
	s := m.render(overviewShape)
	if len(s) <= budget {
		return s
	}
	for _, sh := range compactShapes {
		if s := m.render(sh); len(s) <= budget {
			return s
		}
	}
	return ""
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "..."
}

// render builds the summary text at one level of detail.
func (m *Model) render(sh shape) string {
	var b strings.Builder
	sources := 0
	for _, mod := range m.Modules {
		sources += mod.Files
	}
	fmt.Fprintf(&b, "%d source files in %d modules (model %s).\n", sources, len(m.Modules), shortPrint(m.Fingerprint))

	if sh.languages && len(m.Languages) > 0 {
		type kv struct {
			k string
			n int
		}
		var langs []kv
		for k, n := range m.Languages {
			langs = append(langs, kv{k, n})
		}
		sort.Slice(langs, func(i, j int) bool {
			if langs[i].n != langs[j].n {
				return langs[i].n > langs[j].n
			}
			return langs[i].k < langs[j].k
		})
		parts := make([]string, 0, 4)
		for _, l := range langs[:min(len(langs), 4)] {
			parts = append(parts, fmt.Sprintf("%s %d", l.k, l.n))
		}
		b.WriteString("Languages: " + strings.Join(parts, ", ") + "\n")
	}
	if len(m.TestCommands) > 0 {
		b.WriteString("Run tests: " + strings.Join(m.TestCommands, "; ") + "\n")
	}
	if sh.entries > 0 && len(m.Entries) > 0 {
		paths := make([]string, 0, sh.entries)
		for _, e := range m.Entries[:min(len(m.Entries), sh.entries)] {
			paths = append(paths, e.Path)
		}
		if extra := len(m.Entries) - len(paths); extra > 0 {
			paths = append(paths, fmt.Sprintf("+%d more", extra))
		}
		b.WriteString("Entry points: " + strings.Join(paths, ", ") + "\n")
	}

	if sh.areas > 0 {
		type area struct {
			name    string
			files   int
			biggest *Module
		}
		byName := map[string]*area{}
		for i := range m.Modules {
			mod := &m.Modules[i]
			a := byName[areaOf(mod.Path)]
			if a == nil {
				a = &area{name: areaOf(mod.Path)}
				byName[a.name] = a
			}
			a.files += mod.Files
			if a.biggest == nil || mod.Files > a.biggest.Files {
				a.biggest = mod
			}
		}
		areas := make([]*area, 0, len(byName))
		for _, a := range byName {
			areas = append(areas, a)
		}
		sort.Slice(areas, func(i, j int) bool {
			if areas[i].files != areas[j].files {
				return areas[i].files > areas[j].files
			}
			return areas[i].name < areas[j].name
		})
		b.WriteString("Areas:\n")
		for _, a := range areas[:min(len(areas), sh.areas)] {
			line := fmt.Sprintf("- %s: %d files", a.name, a.files)
			if sh.owners && a.biggest != nil && len(a.biggest.Owners) > 0 {
				line += "; " + formatOwners(a.biggest.Owners[:min(len(a.biggest.Owners), 2)])
			}
			b.WriteString(line + "\n")
		}
		if extra := len(areas) - sh.areas; extra > 0 {
			fmt.Fprintf(&b, "- and %d more areas\n", extra)
		}
	}

	if sh.core > 0 {
		mods := append([]Module(nil), m.Modules...)
		sort.SliceStable(mods, func(i, j int) bool {
			if len(mods[i].UsedBy) != len(mods[j].UsedBy) {
				return len(mods[i].UsedBy) > len(mods[j].UsedBy)
			}
			return mods[i].Path < mods[j].Path
		})
		var parts []string
		for _, mod := range mods[:min(len(mods), sh.core)] {
			if len(mod.UsedBy) == 0 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s (%d)", mod.Path, len(mod.UsedBy)))
		}
		if len(parts) > 0 {
			b.WriteString("Most depended-on modules (users): " + strings.Join(parts, ", ") + "\n")
		}
	}

	if sh.decisions > 0 {
		recorded := m.Decisions
		if len(recorded) > sh.decisions {
			recorded = recorded[len(recorded)-sh.decisions:]
		}
		if len(recorded) > 0 {
			b.WriteString("Recorded decisions:\n")
			for _, d := range recorded {
				line := clip(d.Text, 140)
				if d.Rationale != "" {
					line += " (why: " + clip(d.Rationale, 100) + ")"
				}
				if d.Path != "" {
					line += " [" + d.Path + "]"
				}
				b.WriteString("- " + line + "\n")
			}
		}
	}

	if sh.docs > 0 && len(m.Docs) > 0 {
		var parts []string
		for _, d := range m.Docs {
			if d.ADR {
				continue
			}
			if len(parts) < sh.docs {
				parts = append(parts, d.Path)
			}
		}
		if len(parts) > 0 {
			b.WriteString("Docs: " + strings.Join(parts, ", ") + "\n")
		}
	}
	return b.String()
}

func shortPrint(fp string) string {
	if len(fp) > 8 {
		return fp[:8]
	}
	return fp
}
