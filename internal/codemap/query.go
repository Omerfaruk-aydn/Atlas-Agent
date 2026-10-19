package codemap

import (
	"context"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

// Output limits keep each answer a fraction of reading the files.
const (
	maxFindResults   = 30
	maxListedLinks   = 25
	maxCallerLines   = 60
	maxPathDepth     = 10
	defaultBudget    = 2000
	maxBudget        = 8000
	charsPerToken    = 4
	maxOverviewFiles = 60
)

// Stats summarizes the map.
func (m *Map) Stats() (files, symbols int, byLanguage map[string]int, byConfidence map[Confidence]int) {
	byLanguage = map[string]int{}
	byConfidence = map[Confidence]int{}
	for _, f := range m.Files {
		byLanguage[f.Language]++
		symbols += len(f.Symbols)
	}
	for _, l := range m.links {
		byConfidence[l.Confidence]++
	}
	return len(m.Files), symbols, byLanguage, byConfidence
}

