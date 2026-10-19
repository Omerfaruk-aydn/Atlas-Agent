package codemap

import (
	"fmt"
	"sort"
	"strings"
)

// Limits for the parts of an answer about documents and assets.
const (
	maxOverviewDocs   = 8
	maxListedProblems = 8
	maxListedAssets   = 5
)

// subsystem names fi's community, or "" for a file outside the code graph.
func (m *Map) subsystem(fi int) string {
	if len(m.communities) < 2 || m.community[fi] < 0 || m.community[fi] >= len(m.communities) {
		return ""
	}
	return m.communities[m.community[fi]].Label
}

// describeAsset is what the map measured of an asset: kind, format,
// dimensions and size.
func describeAsset(lang string, a *Asset) string {
	parts := []string{lang}
	if a.Format != "" {
		parts = append(parts, a.Format)
	}
	if a.Width > 0 && a.Height > 0 {
		parts = append(parts, fmt.Sprintf("%dx%d", a.Width, a.Height))
	}
	parts = append(parts, formatBytes(a.Bytes))
	return strings.Join(parts, ", ")
}

// pathUsers returns the links naming fi by path, ordered by file and line.
func (m *Map) pathUsers(fi int) []pathUse {
	uses := append([]pathUse(nil), m.pathIn[fi]...)
	sort.SliceStable(uses, func(i, j int) bool {
		a, b := uses[i], uses[j]
		if a.file != b.file {
			return m.Files[a.file].Path < m.Files[b.file].Path
		}
		return m.Files[a.file].Links[a.link].Line < m.Files[b.file].Links[b.link].Line
	})
	return uses
}

