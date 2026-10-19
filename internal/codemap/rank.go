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

