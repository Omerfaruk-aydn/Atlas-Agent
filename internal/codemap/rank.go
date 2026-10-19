package codemap

import (
	"fmt"
	"path"
	"sort"
)

// community is a group of files that use each other more than the rest:
// a subsystem.
type community struct {
	Label string
	Files []int
	// Weight is the summed rank of its files.
	Weight float64
}

// rank scores files with PageRank over their dependencies, so a file many
// important files depend on ranks high, and scores each definition by the
// rank of the files that use it.
func (m *Map) rank() {
	n := len(m.Files)
	m.fileRank = make([]float64, n)
	m.symbolScore = make([]float64, len(m.symbols))
	if n == 0 {
		return
	}
	outWeight := make([]float64, n)
	incoming := make([][]struct {
		from int
		w    float64
	}, n)
	for key, w := range m.fileLinks {
		if isTestPath(m.Files[key[0]].Path) {
			// Tests use everything they test; that says little about
			// what the project's own code depends on.
			w *= 0.1
		}
		outWeight[key[0]] += w
		incoming[key[1]] = append(incoming[key[1]], struct {
			from int
			w    float64
		}{key[0], w})
	}
	const damping = 0.85
	// Documents and assets are not part of the dependency graph.
	codeFiles := 0
	for i := range m.Files {
		if m.code[i] {
			codeFiles++
		}
	}
	if codeFiles == 0 {
		m.fileScore = make([]float64, n)
		return
	}
	rank := make([]float64, n)
	for i := range rank {
		if m.code[i] {
			rank[i] = 1 / float64(codeFiles)
		}
	}
	next := make([]float64, n)
	for range 50 {
		dangling := 0.0
		for i, r := range rank {
			if outWeight[i] == 0 {
				dangling += r
			}
		}
		base := (1-damping)/float64(codeFiles) + damping*dangling/float64(codeFiles)
		delta := 0.0
		for i := range next {
			if !m.code[i] {
				continue
			}
			sum := 0.0
			for _, e := range incoming[i] {
				sum += rank[e.from] * e.w / outWeight[e.from]
			}
			next[i] = base + damping*sum
			delta += abs(next[i] - rank[i])
		}
		rank, next = next, rank
		if delta < 1e-9 {
			break
		}
	}
	copy(m.fileRank, rank)
	// A file's importance is who depends on it: each file outside the
	// tests that uses it counts, more when that file matters itself.
	// PageRank alone favors small leaves that one entry point imports.
	m.fileScore = make([]float64, n)
	for key := range m.fileLinks {
		if !isTestPath(m.Files[key[0]].Path) {
			m.fileScore[key[1]] += 1 + rank[key[0]]*float64(n)
		}
	}
	for to, links := range m.in {
		for _, id := range links {
			l := m.links[id]
			from := m.fileOf(l.From)
			if !m.code[from] {
				// A document naming a definition says nothing of how much
				// the code depends on it.
				continue
			}
			if from == m.symbols[to].file {
				// Uses inside the defining file count, but less: they say
				// how central a definition is to its file, not to the
				// project.
				m.symbolScore[to] += 0.2 * confidenceWeight(l.Confidence) / float64(l.Candidates)
				continue
			}
			m.symbolScore[to] += (1 + rank[from]*float64(n)) * confidenceWeight(l.Confidence) / float64(l.Candidates)
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// cluster groups files into subsystems with the Louvain method over the
// undirected dependency graph. Files with no dependencies join the
// subsystem most common in their directory, or one of their own.
func (m *Map) cluster() {
	n := len(m.Files)
	m.community = make([]int, n)
	for i := range m.community {
		m.community[i] = -1
	}
	if n == 0 {
		return
	}
	adj := make([]map[int]float64, n)
	for i := range adj {
		adj[i] = map[int]float64{}
	}
	for key, w := range m.fileLinks {
		a, b := key[0], key[1]
		adj[a][b] += w
		adj[b][a] += w
	}
	// Files in one directory usually belong together; a weak chain
	// between them pulls a package into one group without overriding
	// what the uses say.
	for _, files := range m.dirs {
		var code []int
		for _, f := range files {
			if m.code[f] {
				code = append(code, f)
			}
		}
		for k := 1; k < len(code); k++ {
			a, b := code[k-1], code[k]
			if len(adj[a]) > 0 || len(adj[b]) > 0 {
				adj[a][b] += 0.5
				adj[b][a] += 0.5
			}
		}
	}
	assignment := louvain(adj)
	// Number communities by size, largest first, and give isolated files
	// a community by directory.
	groups := map[int][]int{}
	for i, c := range assignment {
		if len(adj[i]) == 0 {
			continue
		}
		groups[c] = append(groups[c], i)
	}
	byDir := map[string]map[int]int{}
	for c, files := range groups {
		for _, fi := range files {
			dir := path.Dir(m.Files[fi].Path)
			if byDir[dir] == nil {
				byDir[dir] = map[int]int{}
			}
			byDir[dir][c]++
		}
	}
	nextID := len(assignment)
	dirGroup := map[string]int{}
	for i := range m.Files {
		if len(adj[i]) > 0 || !m.code[i] {
			continue
		}
		dir := path.Dir(m.Files[i].Path)
		best, count := -1, 0
		for c, k := range byDir[dir] {
			if k > count || k == count && c < best {
				best, count = c, k
			}
		}
		if best < 0 {
			if id, ok := dirGroup[dir]; ok {
				best = id
			} else {
				best = nextID
				dirGroup[dir] = best
				nextID++
			}
		}
		groups[best] = append(groups[best], i)
	}
	type pending struct {
		files  []int
		weight float64
	}
	var all []pending
	for _, files := range groups {
		sort.Ints(files)
		w := 0.0
		for _, fi := range files {
			w += m.fileRank[fi]
		}
		all = append(all, pending{files, w})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].weight != all[j].weight {
			return all[i].weight > all[j].weight
		}
		return all[i].files[0] < all[j].files[0]
	})
	labels := map[string]int{}
	for id, g := range all {
		label := m.commonDir(g.files)
		labels[label]++
		if labels[label] > 1 {
			label += " (" + m.topFileName(g.files) + ")"
		}
		m.communities = append(m.communities, community{Label: label, Files: g.files, Weight: g.weight})
		for _, fi := range g.files {
			m.community[fi] = id
		}
	}
}

// commonDir labels a group of files by the directory most of them are
// in, with how many other directories the group spans.
func (m *Map) commonDir(files []int) string {
	counts := map[string]int{}
	for _, fi := range files {
		counts[path.Dir(m.Files[fi].Path)]++
	}
	best := ""
	for dir, k := range counts {
		if best == "" || k > counts[best] || k == counts[best] && dir < best {
			best = dir
		}
	}
	if best == "." {
		best = "(root)"
	}
	if others := len(counts) - 1; others > 0 {
		if others == 1 {
			return best + " +1 dir"
		}
		return fmt.Sprintf("%s +%d dirs", best, others)
	}
	return best
}

